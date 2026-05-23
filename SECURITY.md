# Security Policy

basement is a self-hosted control plane for S3-compatible storage
backends. Operators run it on infrastructure they own, fronting
clusters they control. We take vulnerability reports seriously and
ship fixes quickly.

## Reporting a vulnerability

**Email**: matthew@pq.io

Please include:

- A description of the issue and the affected version (`docker
  image tag` or `git commit`).
- Steps to reproduce, ideally against a fresh `docker compose -f
  deploy/docker-compose.example.yml up` deployment.
- The impact you observed (e.g. auth bypass, privilege escalation,
  data disclosure, RCE).
- Any proposed mitigation or patch.

We do not currently publish a PGP key. If you need encrypted
transport, mention it in your initial mail and we will agree on a
channel.

Please do **not** open a public GitHub issue for security reports.
The `.github/ISSUE_TEMPLATE/security.yml` form redirects to this
file for the same reason.

## Response SLA

We commit to a **best-effort 48-hour initial response** to security
reports. After triage we will:

1. Confirm receipt and assign a tracking handle.
2. Reproduce the issue and assess severity.
3. Agree on an embargo window with the reporter (default 90 days,
   shorter for in-the-wild exploitation).
4. Ship a patched release and a `docs/release-notes/` entry that
   credits the reporter (unless they request anonymity).

"Best effort" is honest: this is a single-maintainer project. If
the maintainer is travelling or otherwise away, the 48-hour clock
may slip. We will not silently ghost a report.

## Supported versions

We ship security fixes for:

- The **current minor** release (e.g. v1.11.x while v1.11 is the
  latest minor).
- The **previous minor** release (e.g. v1.10.x once v1.11 is out).

Older minors get best-effort backports if the fix is small and the
operator hasn't moved yet. If you're more than two minors behind,
the answer will usually be "please upgrade".

`v1.x` was declared feature-complete with v1.10. `v2.x` (S3 gateway
era) will start a fresh support window when it ships.

## Threat model

basement is designed for a **single operator (or small trusted
team) running it on infrastructure they own**. The trust boundaries:

### What basement trusts

- The host operating system and the operator who deploys it.
- `BASEMENT_DATA_DIR` on disk (default `/var/lib/basement`).
  Anything that can read this directory can recover encrypted
  blobs and the bcrypt-hashed admin password.
- `BASEMENT_JWT_SECRET`. This is the master key — JWT signing key
  for session cookies **and** the input to the AES-256-GCM
  key-derivation function (SHA-256) that wraps per-user S3 secrets
  at rest. Rotate it and every encrypted secret is unreadable.
  Treat it like a database master key.
- The seed admin password — either `BASEMENT_ADMIN_PASSWORD_HASH`
  (bcrypt hash, recommended for production) or `BASEMENT_ADMIN_PASSWORD`
  (plaintext, bcrypted at boot, never persisted) — and any local
  user passwords (stored as bcrypt cost-12 hashes). On a fresh install
  with both unset, the v1.11.0c auto-bootstrap path mints a random
  24-char password and persists it to `{DATA_DIR}/.initial-admin-password`
  (0600); see [`docs/deployment/docker.md`](docs/deployment/docker.md#5-minute-evaluation-v1110c-auto-bootstrap).
- OIDC discovery responses and JWKs from the configured IdP, when
  OIDC is enabled.

### What basement does not trust

- **Backend HTTP responses.** Drivers validate the shape of every
  response from Garage / MinIO / AWS S3 before surfacing it to the
  control plane. A malicious or compromised backend can deny
  service but cannot inject data into basement's own state.
- **Backend audit truth.** basement records its own audit log of
  every mutating action it issues; it does not replay the
  backend's audit log as authoritative.
- **Backend permissions as policy.** basement does not re-invent
  cluster permissions. Whether a key can read a bucket is the
  backend's call. basement surfaces what the backend reports.
- **User-supplied URLs and inputs.** All operator-supplied
  endpoints, webhook targets, and KMS key IDs are validated and
  scoped before use; webhook bodies are HMAC-SHA256 signed so the
  receiver can verify provenance.

### What basement encrypts at rest

- **Per-user S3 secret keys** (the `/files/keys` keychain):
  AES-256-GCM with a 12-byte random nonce, key derived as
  `sha256(BASEMENT_JWT_SECRET)`. Wire format is
  `nonce(12) || gcm-ciphertext(plaintext + auth-tag)`. Tamper any
  byte and `Open` fails closed. See
  `internal/store/crypto.go`.
- **Local user passwords**: bcrypt cost-12 (Go
  `golang.org/x/crypto/bcrypt`).
- **Service-account secrets** (`BMNT...:secret` bearer
  credentials): bcrypt of the secret half is stored; the
  plaintext is shown to the operator exactly once at mint and
  never persisted by basement. See `internal/serviceaccount/store.go`.
- **Cluster admin tokens** (Garage admin tokens etc.): AES-256-GCM,
  same scheme as the per-user S3 keys.

### What basement encrypts that you might think we do, but we don't

- **KMS key IDs are stored plaintext.** A KMS key ID is a public
  identifier, not a secret — the secret is the key material held by
  the KMS itself. We surface the ID for the SSE-KMS UI and pass it
  through to the backend without trying to obscure it.
- **Object data is never proxied** by basement through v1.x.
  Drivers issue S3 calls but the bytes don't traverse the control
  plane. The v2.0 S3 gateway will change this for inbound writes;
  the threat model will be revised then.

### What basement does not log

- **Object contents.** basement does not inspect, parse, or log
  user object payloads.
- **User passwords** in plaintext (only bcrypt hashes hit disk).
- **Bearer secret halves** after the one-time mint reveal.
- **S3 secret keys** in plaintext after the operator pastes them
  into the keychain.

The audit log records actor, capability, scope, and result — not
payloads.

## Public disclosure policy

We follow standard responsible-disclosure norms:

- Default embargo of **90 days** from report receipt or until a
  fixed release is published, whichever comes first.
- Coordinated disclosure with the reporter — they choose how / if
  to publish their writeup, and we credit them in the release
  notes unless they decline.
- If we observe active in-the-wild exploitation, the embargo may
  collapse to "patch and ship now"; we will tell the reporter
  before that happens.
- If a report turns out to be a feature request or a known
  limitation, we will say so and may move the discussion to a
  public issue with the reporter's consent.

## SBOM

Every tagged release publishes a CycloneDX-format Software Bill of
Materials (SBOM) generated by [syft](https://github.com/anchore/syft)
as a GitHub release artifact (`basement-<version>-sbom.cdx.json`).
See `.github/workflows/sbom.yml`. Use it to track which versions of
your basement install ship which transitive dependencies.

basement does not currently produce reproducible builds. The SBOM
is the closest thing to a supply-chain manifest we offer today.

## Related docs

- [`CONTRIBUTING.md`](./CONTRIBUTING.md) — contribution terms +
  DCO sign-off.
- [`LICENSE`](./LICENSE) — AGPL-3.0.
- [`docs/configuration.md`](./docs/configuration.md) — production
  environment variables (including the security-sensitive ones
  named in the threat model above).
