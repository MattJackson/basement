// Package store implements persistent storage for connection records.
//
// Connections are stored in connections.json under BASEMENT_DATA_DIR.
// Each connection represents a backend driver configuration (garage, garage-v1, aws-s3).
//
// Encryption at rest: sensitive keys in Connection.Config (admin_token,
// secret_key, s3_secret_key, auth_token) are AES-GCM encrypted into a
// per-record ConfigEnc blob via crypto.go using a key derived from the
// JWT signing secret. Non-sensitive keys (admin_url, region, endpoint,
// access key IDs) stay in plaintext in Config on disk. In memory and
// across API boundaries the Config map is unified — callers see one
// decrypted view and don't need to know the split exists.
//
// Migration: on Open, if a Connection has plaintext sensitive keys in
// Config but ConfigEnc is empty, the load path encrypts those keys
// into ConfigEnc, drops them from the plaintext Config persisted on
// disk, and re-saves. Idempotent on repeat runs — once ConfigEnc is
// populated and Config carries only non-sensitive keys on disk, the
// scan is a no-op.
//
// Back-compat: OpenConnections(dataDir) keeps the v0.2.x signature
// (no encryption — tests use it). Production wires
// OpenConnectionsWithKey(dataDir, jwtSecret) from cmd/basement-server to
// turn on at-rest encryption.
package store

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Supported drivers for validation.
const (
	DriverGarage    = "garage"
	DriverGarageV1  = "garage-v1"
	DriverAWSS3     = "aws-s3"
	DriverMinio     = "minio"
)

// SupportedDrivers is the set of drivers that can be used in connections.
var SupportedDrivers = map[string]bool{
	DriverGarage:   true,
	DriverGarageV1: true,
	DriverAWSS3:    true,
	DriverMinio:    true,
}

// sensitiveConfigKeys classifies which Config keys must be encrypted at
// rest. Access-key IDs (access_key_id, access_key, s3_access_key) are
// deliberately NOT in this set — they appear in S3 request headers in
// clear and only pair with a secret to authenticate, so they're "public-
// ish" like a username. The secret half (secret_key, s3_secret_key,
// admin_token, auth_token) is what we must guard.
//
// Adding a new sensitive key: extend this map AND ship a follow-up
// migration test asserting plaintext on disk converts to ciphertext.
// Removing one is a breaking change — operators upgrading would find
// their on-disk ConfigEnc still holds the key and we'd merge it back
// into Config on load; if you must remove, ship a one-shot purge.
var sensitiveConfigKeys = map[string]bool{
	"admin_token":   true,
	"secret_key":    true,
	"s3_secret_key": true,
	"auth_token":    true,
}

// Connection represents a backend connection configuration.
//
// ConfigEnc carries the AES-GCM ciphertext of the sensitive subset of
// Config (per sensitiveConfigKeys) under the legacy JWT-derived key
// (v1.0.0a). It is `json:"-"` on this public struct so API responses
// keep the v0.2.x shape callers expect — a single Config map. Disk
// persistence routes through connectionDisk which DOES marshal the
// field. See load/save below.
//
// ConfigEncCSK (v1.12.0b / ADR-0007) carries the same JSON-marshalled
// sensitive subset re-encrypted under the cluster's CSK once the
// operator has bootstrapped per-cluster envelope encryption AND the
// first unlock has run the lazy migration. The legacy ConfigEnc is
// kept as a bridge — load() continues to decrypt under JWT until the
// CSK-aware load path lands in a follow-up cycle. Future code that
// needs CSK-protected secrets reads ConfigEncCSK via the clustersecret
// manager when the cluster is unlocked; everything else uses the
// existing JWT path.
//
// Invariant: in-memory Config is the unified view (plaintext + the
// decrypted sensitive subset from ConfigEnc). On-disk Config carries
// only the non-sensitive subset; ConfigEnc holds the JWT-encrypted
// blob; ConfigEncCSK (when populated) holds the CSK-encrypted parallel.
type Connection struct {
	ID           string            `json:"id"`              // UUID
	Label        string            `json:"label"`           // operator-set, mutable, unique case-insensitive
	Driver       string            `json:"driver"`          // "garage" | "garage-v1" | "aws-s3"
	Config       map[string]string `json:"config"`          // per-driver keys: adminUrl, adminToken, region, accessKey, secretKey, endpoint
	ConfigEnc    []byte            `json:"-"`               // AES-GCM(json(sensitive-subset)) under JWT-derived key; never on the API wire
	ConfigEncCSK []byte            `json:"-"`               // AES-GCM(json(sensitive-subset)) under CSK (v1.12.0b+); never on the API wire
	Color        string            `json:"color,omitempty"` // hex; default "#C9874B" if empty
	Owner        string            `json:"owner"`           // "org" always for v0.2.0
	CreatedAt    time.Time         `json:"createdAt"`
}

// connectionDisk is the on-disk JSON shape: identical to Connection but
// with ConfigEnc + ConfigEncCSK marshalled. Used only by load/save —
// never returned to callers. Keeping the struct private keeps the API
// JSON shape sealed.
type connectionDisk struct {
	ID           string            `json:"id"`
	Label        string            `json:"label"`
	Driver       string            `json:"driver"`
	Config       map[string]string `json:"config"`
	ConfigEnc    []byte            `json:"configEnc,omitempty"`
	ConfigEncCSK []byte            `json:"configEncCSK,omitempty"`
	Color        string            `json:"color,omitempty"`
	Owner        string            `json:"owner"`
	CreatedAt    time.Time         `json:"createdAt"`
}

// Redacted returns a copy of c with every sensitive Config key removed.
// Use this for any wire response — listClustersHandler, getClusterHandler,
// any handler that hands a Connection back to a caller. The unredacted
// Config (with admin_token etc) stays in memory for driver dispatch only.
//
// v1.13.28: introduced after a live smoke caught admin_token leaking
// through GET /api/v1/admin/clusters to user-mode callers.
func (c Connection) Redacted() Connection {
	if c.Config == nil {
		return c
	}
	cfg := make(map[string]string, len(c.Config))
	for k, v := range c.Config {
		if sensitiveConfigKeys[k] {
			continue
		}
		cfg[k] = v
	}
	c.Config = cfg
	return c
}

// RedactConnections is a slice variant of Redacted for List responses.
func RedactConnections(in []Connection) []Connection {
	out := make([]Connection, len(in))
	for i, c := range in {
		out[i] = c.Redacted()
	}
	return out
}

// Connections interface defines the CRUD operations for connection records.
type Connections interface {
	List(ctx context.Context) ([]Connection, error)
	Get(ctx context.Context, id string) (Connection, error)
	Create(ctx context.Context, c Connection) (Connection, error) // assigns ID + createdAt
	Update(ctx context.Context, id string, patch Connection) (Connection, error)
	Delete(ctx context.Context, id string) error
	// Convenience for boot-time auto-seed:
	Count(ctx context.Context) (int, error)

	// SwapClusterSecret atomically replaces the CSK-encrypted
	// sensitive-subset blob (ConfigEncCSK) for cid. Used by the
	// v1.12.0b lazy migration from JWT-encrypted ConfigEnc to
	// CSK-encrypted ConfigEncCSK (ADR-0007).
	//
	// Idempotency: the swap only fires when the on-disk ConfigEncCSK
	// byte-matches oldConfigEnc. If another goroutine raced and
	// already migrated, the supplied oldConfigEnc won't match the
	// fresh on-disk value and SwapClusterSecret returns nil without
	// touching disk — safe for concurrent first-unlock by two
	// admins. Pass nil/empty oldConfigEnc when initiating the first
	// migration (the field starts empty).
	//
	// Atomic: the rewrite goes through the same tmp+fsync+rename
	// pipeline as every other store mutation. The legacy ConfigEnc
	// field is NEVER touched here — the JWT-encrypted bridge stays
	// in place so the existing load() path keeps working until a
	// future cycle teaches load to read ConfigEncCSK.
	//
	// Returns an error if cid is not found, if the on-disk read
	// fails, or if the persist fails (in which case the in-memory
	// cache is rolled back to match disk).
	SwapClusterSecret(ctx context.Context, cid string, oldConfigEnc, newConfigEnc []byte) error
}

// store implements Connections using JSON file persistence.
type store struct {
	dataDir    string
	connPath   string
	jwtSecret  []byte // nil ⇒ encryption disabled (legacy OpenConnections path / tests)
	connsMu    sync.RWMutex
	connsCache []Connection
}

// OpenConnections opens or creates the connections store at dataDir
// WITHOUT at-rest encryption. Retained for v0.2.x source compat — tests
// use this path. Production callers must use OpenConnectionsWithKey
// to turn on AES-GCM encryption of admin_token and secret_key.
func OpenConnections(dataDir string) (Connections, error) {
	return openConnections(dataDir, nil)
}

// OpenConnectionsWithKey opens or creates the connections store at
// dataDir with at-rest encryption keyed off jwtSecret. The actual
// AES-256 key is sha256(jwtSecret) — see crypto.go. jwtSecret must be
// non-empty; cfg validation enforces ≥32 bytes in real deployments.
//
// On open this runs a silent migration that encrypts any plaintext
// sensitive keys discovered in connections.json (left over from
// pre-v1.0.0a deployments) and rewrites the file. Idempotent on
// subsequent boots.
func OpenConnectionsWithKey(dataDir string, jwtSecret []byte) (Connections, error) {
	if len(jwtSecret) == 0 {
		return nil, fmt.Errorf("OpenConnectionsWithKey: empty jwtSecret")
	}
	return openConnections(dataDir, append([]byte(nil), jwtSecret...))
}

// openConnections is the shared constructor. jwtSecret may be nil to
// signal encryption-off mode; load/save degrade to plaintext in that
// case so the OpenConnections test surface stays green.
func openConnections(dataDir string, jwtSecret []byte) (Connections, error) {
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("creating data dir: %w", err)
	}

	s := &store{
		dataDir:    dataDir,
		connPath:   filepath.Join(dataDir, "connections.json"),
		jwtSecret:  jwtSecret,
		connsCache: make([]Connection, 0),
	}

	if err := s.load(); err != nil {
		return nil, fmt.Errorf("loading existing connections: %w", err)
	}

	return s, nil
}

// load reads connections.json into the cache, decrypts ConfigEnc into
	// the in-memory Config map, and (one-shot per record) migrates any
	// plaintext sensitive keys to ConfigEnc by rewriting the file.
	//
	// Encryption-off mode (jwtSecret == nil) skips both decrypt and
	// migration; on-disk shape stays as Connection JSON, plaintext.
	func (s *store) load() error {
		disk, err := loadJSON[[]connectionDisk](s.connPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("loading connections: %w", err)
		}

		if err != nil { // file missing
			s.connsMu.Lock()
			s.connsCache = make([]Connection, 0)
			s.connsMu.Unlock()
			return nil
		}

		cache := make([]Connection, 0, len(disk))
		migrated := false
		droppedLegacyCSK := 0
		for _, d := range disk {
			c := Connection{
				ID:           d.ID,
				Label:        d.Label,
				Driver:       d.Driver,
				Color:        d.Color,
				Owner:        d.Owner,
				CreatedAt:    d.CreatedAt,
				Config:       cloneStringMap(d.Config),
				ConfigEnc:    d.ConfigEnc,
				ConfigEncCSK: d.ConfigEncCSK,
			}
			if c.Config == nil {
				c.Config = map[string]string{}
			}

			if s.jwtSecret != nil {
				// v2.0.0-beta.2: Check for legacy JWT-encrypted credentials
				// with no CSK parallel — these clusters are dropped per [[v2_clean_break]].
				if len(c.ConfigEnc) > 0 && len(c.ConfigEncCSK) == 0 {
					droppedLegacyCSK++
					continue
				}

				// Decrypt any existing ConfigEnc into the in-memory Config so
				// callers see a unified view.
				if len(c.ConfigEnc) > 0 {
					dec, derr := decryptSensitiveMap(c.ConfigEnc, s.jwtSecret)
					if derr != nil {
						return fmt.Errorf("decrypting ConfigEnc for connection %q: %w", c.ID, derr)
					}
					for k, v := range dec {
						c.Config[k] = v
					}
				}

				// Migration: if the on-disk Config (d.Config — BEFORE we
				// merged decrypted ConfigEnc in) carries plaintext sensitive
				// keys, flag a rewrite. save() will split them out.
				//
				// We use d.Config rather than c.Config so post-merge keys
				// (which always look "plaintext" in c.Config because
				// decryption put them there) don't trigger a needless
				// re-save on every boot. Result: idempotent — second boot
				// with already-encrypted records is a no-op.
				for k := range d.Config {
					if sensitiveConfigKeys[k] {
						migrated = true
						break
					}
				}
			}

			cache = append(cache, c)
		}

		s.connsMu.Lock()
		s.connsCache = cache
		s.connsMu.Unlock()

		if droppedLegacyCSK > 0 {
			fmt.Fprintf(os.Stderr, "[WARN] Dropped %d connection(s) with legacy JWT-encrypted credentials (ConfigEnc but no ConfigEncCSK); re-add via /admin/connections per v2.0.0-beta.2 [[v2_clean_break]]\n", droppedLegacyCSK)
		}

		if migrated {
			s.connsMu.Lock()
			err := s.saveLocked()
			s.connsMu.Unlock()
			if err != nil {
				return fmt.Errorf("re-saving connections after at-rest migration: %w", err)
			}
		}

		return nil
	}

// save writes the connections cache to disk atomically, encrypting
// sensitive keys on the way out. Caller must hold connsMu (write).
func (s *store) save() error {
	return s.saveLocked()
}

// saveLocked is save assuming connsMu is already held (Lock, not RLock).
// load() and the mutating ops call this directly; encryption errors
// surface as a save failure rather than corrupt-data-on-disk.
//
// As a side effect, the cache's ConfigEnc field is back-filled with
// the freshly-computed ciphertext so subsequent Get/List calls see
// the same legacy blob that's on disk — required by the v1.12.0b
// migration helper which reads conn.ConfigEnc to know what to
// re-encrypt under CSK. Without this back-fill the cache and disk
// would disagree on the ConfigEnc field after every Create/Update
// (cache empty, disk populated).
func (s *store) saveLocked() error {
	disk := make([]connectionDisk, 0, len(s.connsCache))
	for i := range s.connsCache {
		dc, err := s.toDisk(s.connsCache[i])
		if err != nil {
			return fmt.Errorf("preparing connection %q for disk: %w", s.connsCache[i].ID, err)
		}
		disk = append(disk, dc)
		// Keep the in-memory ConfigEnc / ConfigEncCSK in lockstep
		// with what just went to disk. Required so the v1.12.0b
		// CSK migration helper (api.maybeMigrateLegacyClusterSecret)
		// sees the legacy ciphertext via Get without a fresh load.
		s.connsCache[i].ConfigEnc = dc.ConfigEnc
		s.connsCache[i].ConfigEncCSK = dc.ConfigEncCSK
	}
	return saveJSON(s.connPath, disk)
}

// toDisk splits a Connection's unified Config into the plaintext on-disk
// subset + an encrypted blob holding the sensitive subset. Encryption-
// off mode (s.jwtSecret == nil) round-trips the Config plaintext as-is.
//
// ConfigEncCSK is preserved verbatim — toDisk does NOT regenerate it;
// the CSK blob is only ever (re)written by SwapClusterSecret which
// owns the migration semantics. This keeps a routine Update/Create
// from accidentally clobbering an existing migration record.
func (s *store) toDisk(c Connection) (connectionDisk, error) {
	if s.jwtSecret == nil {
		return connectionDisk{
			ID:           c.ID,
			Label:        c.Label,
			Driver:       c.Driver,
			Config:       cloneStringMap(c.Config),
			ConfigEnc:    nil,
			ConfigEncCSK: c.ConfigEncCSK,
			Color:        c.Color,
			Owner:        c.Owner,
			CreatedAt:    c.CreatedAt,
		}, nil
	}

	plain := map[string]string{}
	sensitive := map[string]string{}
	for k, v := range c.Config {
		if sensitiveConfigKeys[k] {
			sensitive[k] = v
		} else {
			plain[k] = v
		}
	}

	var enc []byte
	if len(sensitive) > 0 {
		raw, err := json.Marshal(sensitive)
		if err != nil {
			return connectionDisk{}, fmt.Errorf("marshalling sensitive subset: %w", err)
		}
		enc, err = encryptSecret(raw, s.jwtSecret)
		if err != nil {
			return connectionDisk{}, fmt.Errorf("encrypting sensitive subset: %w", err)
		}
	}

	return connectionDisk{
		ID:           c.ID,
		Label:        c.Label,
		Driver:       c.Driver,
		Config:       plain,
		ConfigEnc:    enc,
		ConfigEncCSK: c.ConfigEncCSK,
		Color:        c.Color,
		Owner:        c.Owner,
		CreatedAt:    c.CreatedAt,
	}, nil
}

// decryptSensitiveMap is the inverse of the sensitive-subset
// marshal+encrypt path in toDisk. Returns the decrypted subset or an
// error (truncated blob / wrong key / tampered ciphertext).
func decryptSensitiveMap(enc, key []byte) (map[string]string, error) {
	if len(enc) == 0 {
		return map[string]string{}, nil
	}
	plain, err := decryptSecret(enc, key)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(plain), &out); err != nil {
		return nil, fmt.Errorf("unmarshalling decrypted sensitive subset: %w", err)
	}
	return out, nil
}

// cloneStringMap copies a map; returns an empty non-nil map for a nil
// input so callers can always range/write safely.
func cloneStringMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// List returns all connections. Callers receive a deep copy with
// decrypted Config maps.
func (s *store) List(ctx context.Context) ([]Connection, error) {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()

	result := make([]Connection, len(s.connsCache))
	for i, c := range s.connsCache {
		result[i] = c
		result[i].Config = cloneStringMap(c.Config)
	}
	return result, nil
}

// Get returns a single connection by ID. Returns error if not found.
func (s *store) Get(ctx context.Context, id string) (Connection, error) {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()

	for _, c := range s.connsCache {
		if c.ID == id {
			out := c
			out.Config = cloneStringMap(c.Config)
			return out, nil
		}
	}

	return Connection{}, fmt.Errorf("connection not found: %s", id)
}

// Create adds a new connection. Assigns UUID and createdAt timestamp.
// Validates driver is supported and label is unique (case-insensitive).
func (s *store) Create(ctx context.Context, c Connection) (Connection, error) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	// Validate driver
	if !SupportedDrivers[c.Driver] {
		return Connection{}, fmt.Errorf("unsupported driver: %q", c.Driver)
	}

	// Validate label is non-empty and unique (case-insensitive trimmed)
	label := strings.TrimSpace(strings.ToLower(c.Label))
	if label == "" {
		return Connection{}, fmt.Errorf("label must be non-empty")
	}

	for _, existing := range s.connsCache {
		if strings.TrimSpace(strings.ToLower(existing.Label)) == label {
			return Connection{}, fmt.Errorf("duplicate label (case-insensitive): %q", c.Label)
		}
	}

	// Assign ID if not provided
	if c.ID == "" {
		b := make([]byte, 16)
		if _, err := rand.Read(b); err != nil {
			return Connection{}, fmt.Errorf("generating UUID: %w", err)
		}
		c.ID = uuid.UUID(b).String()
	}

	// Assign createdAt if not provided
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now().UTC()
	}

	// Set default color if empty
	if c.Color == "" {
		c.Color = "#C9874B"
	}

	// Normalise Config to non-nil so cache invariant holds.
	if c.Config == nil {
		c.Config = map[string]string{}
	} else {
		c.Config = cloneStringMap(c.Config)
	}

	s.connsCache = append(s.connsCache, c)

	if err := s.saveLocked(); err != nil {
		// Roll back the in-memory append so a save failure isn't
		// observable to subsequent List/Get calls.
		s.connsCache = s.connsCache[:len(s.connsCache)-1]
		return Connection{}, fmt.Errorf("persisting connection: %w", err)
	}

	out := c
	out.Config = cloneStringMap(c.Config)
	return out, nil
}

// Update modifies an existing connection by ID. Returns error if not found.
// Only non-empty fields in the patch are applied (partial update).
func (s *store) Update(ctx context.Context, id string, patch Connection) (Connection, error) {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	for i := range s.connsCache {
		if s.connsCache[i].ID == id {
			conn := &s.connsCache[i]

			// Snapshot for rollback on save failure.
			before := *conn
			before.Config = cloneStringMap(conn.Config)

			// Apply patch fields if non-empty/non-nil
			if patch.Label != "" {
				label := strings.TrimSpace(strings.ToLower(patch.Label))
				if label == "" {
					return Connection{}, fmt.Errorf("label must be non-empty")
				}

				// Check uniqueness (excluding self)
				for _, existing := range s.connsCache {
					if existing.ID != id && strings.TrimSpace(strings.ToLower(existing.Label)) == label {
						return Connection{}, fmt.Errorf("duplicate label (case-insensitive): %q", patch.Label)
					}
				}

				conn.Label = patch.Label
			}

			if patch.Driver != "" && SupportedDrivers[patch.Driver] {
				conn.Driver = patch.Driver
			} else if patch.Driver != "" && !SupportedDrivers[patch.Driver] {
				return Connection{}, fmt.Errorf("unsupported driver: %q", patch.Driver)
			}

			if patch.Config != nil {
				conn.Config = cloneStringMap(patch.Config)
			}

			if patch.Color != "" {
				conn.Color = patch.Color
			}

			if err := s.saveLocked(); err != nil {
				// Roll back so a save failure leaves the in-memory record
				// matching disk.
				*conn = before
				return Connection{}, fmt.Errorf("persisting update: %w", err)
			}

			out := *conn
			out.Config = cloneStringMap(conn.Config)
			return out, nil
		}
	}

	return Connection{}, fmt.Errorf("connection not found: %s", id)
}

// Delete removes a connection by ID. Returns error if not found.
func (s *store) Delete(ctx context.Context, id string) error {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	for i := range s.connsCache {
		if s.connsCache[i].ID == id {
			removed := s.connsCache[i]
			s.connsCache = append(s.connsCache[:i], s.connsCache[i+1:]...)
			if err := s.saveLocked(); err != nil {
				// Restore so an errored Delete doesn't silently mutate
				// in-memory state.
				s.connsCache = append(s.connsCache[:i], append([]Connection{removed}, s.connsCache[i:]...)...)
				return fmt.Errorf("persisting delete: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("connection not found: %s", id)
}

// SwapClusterSecret implements Connections. See the interface doc for
// the contract; v1.12.0b / ADR-0007 wires the lazy migration of a
// cluster's sensitive Config blob from the JWT-derived key to the
// per-cluster CSK on first unlock.
//
// The swap targets ConfigEncCSK and never mutates the legacy
// ConfigEnc; the JWT-encrypted bridge stays in place so an
// already-deployed v1.12.0a process restarted on disk written by
// v1.12.0b still loads cleanly via the existing JWT path. A future
// cycle teaches load() to prefer ConfigEncCSK when CSK is wired and
// the operator has unlocked, at which point ConfigEnc can be retired
// in a one-shot purge.
func (s *store) SwapClusterSecret(ctx context.Context, cid string, oldConfigEnc, newConfigEnc []byte) error {
	s.connsMu.Lock()
	defer s.connsMu.Unlock()

	for i := range s.connsCache {
		if s.connsCache[i].ID != cid {
			continue
		}
		conn := &s.connsCache[i]

		// Idempotency / concurrent-migration guard: only swap when the
		// expected old value matches the current on-disk-equivalent.
		// bytes.Equal handles nil vs empty consistently — callers pass
		// nil when they expect "no CSK blob yet" which matches a
		// freshly-loaded connection whose ConfigEncCSK is also nil.
		if !bytes.Equal(conn.ConfigEncCSK, oldConfigEnc) {
			return nil
		}

		// Snapshot for rollback on save failure so callers don't
		// observe a half-applied swap if the rename to final fails.
		before := conn.ConfigEncCSK

		conn.ConfigEncCSK = append([]byte(nil), newConfigEnc...)

		if err := s.saveLocked(); err != nil {
			conn.ConfigEncCSK = before
			return fmt.Errorf("persisting cluster secret swap: %w", err)
		}
		return nil
	}

	return fmt.Errorf("connection not found: %s", cid)
}

// Count returns the number of connections.
func (s *store) Count(ctx context.Context) (int, error) {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()

	return len(s.connsCache), nil
}

// GenerateID creates a new UUID for connection IDs.
func GenerateID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating UUID: %w", err)
	}
	return uuid.UUID(b).String(), nil
}

// GenerateToken creates a random hex token for authentication.
func GenerateToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
