# Screenshot Shot List — basement v0.5.0

Structured guide for capturing PNG/WebP images used in README, blog posts, and launch materials. **You do NOT capture these yourself** — operator with access to live deploy at `https://basement.pq.io` runs through this list in one sitting.

---

## Shot 1 — `clusters-list.png` (HERO)

**Where used**: README hero image (line 12), most important shot

**Setup**:
- 3 clusters added via `/admin/clusters`:
  - Label **"home"** · driver Garage v1 · color blue (#3B82F6)
  - Label **"work"** · driver MinIO · color orange (#F97316)  
  - Label **"offsite"** · driver AWS S3 · color green (#10B981)
- All three healthy (green status pills per `ClusterRow` component)
- Each cluster has at least 2-5 buckets to populate the "Buckets" column with non-zero counts

**Composition**:
- Window: 1440 × 900, full browser chrome shown for context
- Crop: full window OR tight to page content (operator decides; pick whichever frames best)
- Highlights: none — screen speaks for itself
- Dark mode (project default per `tailwind.config.js` + `App.tsx`)

**Sample data hints**:
- Labels: `"home" / "work" / "offsite"` reads like real operator setup
- Bucket counts: varied (e.g., 4 / 12 / 7) — not all same number
- Driver badges visible: Garage v1, MinIO, AWS S3 per `drivers.go`

---

## Shot 2 — `bucket-detail.png`

**Where used**: README features section ("Bucket + Key admin"), comparison row "Bucket admin"

**Setup**:
- Pick bucket with meaningful stats via `/admin/clusters/{cid}/buckets/{id}`:
  - Real-looking name like `"family-photos"` or `"work-backups"`
  - 1000+ objects, 50+ GB, 1-2 keys with mixed permissions visible in keys-access table
  - Quotas card populated (hard/soft limits)

**Composition**:
- Window: 1440 × 900
- Show: header (bucket name + cluster badge), stats card (size, objects, unfinished uploads), quotas card, keys-access table
- Optional: an "Edit" button visible to demonstrate write capability

**Sample data hints**:
- Don't show real creds. Access key IDs should be test/placeholder strings like `"AKIA…1234"` per `keys.go`
- Bucket stats realistic but not production-sensitive values

---

## Shot 3 — `key-permission-grid.png`

**Where used**: README features section, blog post (T2.38b shipped permission grid EDIT mode)

**Setup**:
- Key with grants on 3-5 buckets via `/admin/clusters/{cid}/keys/{id}`
- Open the permission grid in **EDIT mode** (checkbox grid per `KeyPermissionGrid.tsx`)
- At least one row of each state: Read only, Write+Owner, all unchecked

**Composition**:
- Window: 1440 × 900
- Show: checkbox grid with mixed states clearly visible
- "Save" / "Cancel" buttons visible below per component design

**Sample data hints**:
- Bucket names readable: `"photos" / "backups" / "docs" / "archive" / "scratch"`
- Permissions matrix shows R/W/O columns clearly

---

## Shot 4 — `oidc-login.png`

**Where used**: README features section ("OIDC + local password"), blog post "auth" (config: `configuration.md:2`)

**Setup**:
- Env vars set: `BASEMENT_OIDC_ISSUER`, `BASEMENT_OIDC_PROVIDER_NAME = "Authentik"`
- Log out so login screen appears at `/admin/login`
- Login form visible with username/password + divider + OIDC button

**Composition**:
- Window: 1440 × 900 OR cropped to just centered login card
- Show: username/password form + divider + "Sign in with Authentik" button
- Dark mode (default theme)

**Sample data hints**:
- Filled-in fields OK if they show test creds (`"admin"` in username, password masked as dots)
- No real secrets visible — provider name is `"Authentik"` or similar

---

## Shot 5 — `cross-cluster-buckets.png`

**Where used**: README features section ("Multi-cluster admin")

**Setup**:
- 3 clusters with buckets each (same setup as Shot 1)
- Navigate to `/admin/buckets` (aggregated cross-cluster list per `buckets.tsx`)
- Buckets from all 3 clusters visible, "Cluster" column shows color dot + label per row

**Composition**:
- Window: 1440 × 900
- Show: table with mixed cluster rows; Cluster filter dropdown closed; search box empty so all rows show
- Optionally: one frame with ClusterFilter dropdown open to show filter affordance

**Sample data hints**:
- Mix of bucket sizes (some big, some small) for visual variety in Size column
- Cluster column clearly shows color-coded labels from Shot 1 setup

---

## Shot 6 — `empty-state-welcome.png`

**Where used**: blog post "first-run experience" + onboarding docs

**Setup**:
- Fresh install with empty `connections.json` (no env-seed)
- Log in as admin — lands on welcome card at `/admin`
- Welcome card visible: basement logo + "Welcome to basement" + prominent "Add your first cluster" CTA

**Composition**:
- Window: 1440 × 900
- Show: centered welcome card with full branding and CTA button
- Dark mode (default theme)

**Sample data hints**:
- None — the screen IS the data. No clusters, no buckets, just clean onboarding state.

---

## Shot 7 — `layout-editor.png` (optional, Garage-specific)

**Where used**: README features section ("Layout editor (Garage)"), confirmed shipped per `scripts/README.md:150`

**Setup**:
- Garage cluster with 3+ nodes via `/admin/clusters/{cid}/layout`
- Edit one row (capacity bump or zone change), click Stage
- Diff appears below in "Staged changes" card

**Composition**:
- Window: 1440 × 900
- Show: nodes table on top, staged-changes card below with diff entries (`+` / `~` / `-`)
- "Apply" + "Revert" buttons visible per `LayoutEditor.tsx`

**Sample data hints**:
- Node IDs are long hex — UI should truncate display (verify columns aren't overflowing)
- Diff shows meaningful changes: capacity deltas, zone moves

---

## Shot 8 — `comparison-collage.png` (optional, blog-only)

**Where used**: blog post hero "post-MinIO landscape" (NOT referenced in README or public repo)

**Setup**: composite — 4 screenshots of competing UIs arranged in 2×2 grid:
- Top-left: MinIO Console ("read-only object browser")
- Top-right: OpenMaxIO ("MinIO fork, single-backend")
- Bottom-left: garage-webui / Noooste/garage-ui ("Garage-only admin")
- Bottom-right: **basement** (visual hierarchy: "and here's where we land")

**Composition**:
- 1920 × 1080 collage
- Captions identify each project + tagline (factual, not pejorative)
- If competitor UI on real domain, blur URL bar for cleanliness

**Sample data hints**:
- Don't reuse competitor logos in way that suggests endorsement
- Keep captions factual: "MinIO Console: web UI for MinIO clusters" vs "basement: multi-backend admin with Garage v2 support"

---

## Process notes for the operator

After capturing:

1. **Save as PNG** (lossless) or WebP (smaller, high-quality via `cwebp`)
2. **Target file sizes < 300KB each** post-compression
   - Use `pngcrush` for PNGs: `pngcrush -reduce input.png output.png`
   - Use `cwebp` for WebP: `cwebp -q 80 input.png -o output.webp`
3. **Drop into `docs/screenshots/<slug>.png`** (paths already referenced by README + COMPOSE prompts)
4. **No further wiring needed** — README line 12 references `./docs/screenshots/clusters-list.png`, line 51-52 points to this shot list for descriptions
5. **Commit images with a separate small commit**:
   ```bash
   git -C /Users/mjackson/Developer/basement add docs/screensshots/*.png
   git -C /Users/mjackson/Developer/basement commit -m "docs: add screenshots for README + blog posts (POLISH.SCREENSHOTS)"
   ```

---

## Anti-fabrication gates (NON-NEGOTIABLE)

- Every numeric / symbolic claim has a pasted-output citation
- Every existing-code reference cites `file:line`
- When the source doesn't say what you need, mark **OPEN** — DO NOT invent
- Don't reinterpret a prompt to fit available evidence; flag the gap
- `git status --short` before commit
- `git -C <absolute-path>` always; NEVER `cd <path> && git ...`
- Stage by file name. NEVER `git add .` or `-A`
- NO `--no-verify` on commits
- NO `Co-authored-by` text anywhere

---

## Feature verification checklist (v0.5.0 shipped)

Before capturing, verify these features are live on `https://basement.pq.io`:

| Shot | Feature | Source citation | Status |
|------|---------|-----------------|--------|
| 1 | Clusters list with driver badges | README:24 (four drivers ship in v0.5.0) | ✅ shipped |
| 2 | Bucket detail + keys-access table | README:36 (bucket admin CRUD, per-bucket permissions) | ✅ shipped |
| 3 | Key permission grid EDIT mode | T2.38b (permission grid shipped) | ✅ shipped |
| 4 | OIDC login screen | configuration.md:2 (OIDC config v1.3+) + README:34 | ✅ shipped |
| 5 | Cross-cluster buckets list | README:30 (multi-cluster admin) + `buckets.tsx` | ✅ shipped |
| 6 | Empty-state welcome card | Quickstart:69 (env-seeded admin on fresh install) | ✅ shipped |
| 7 | Layout editor for Garage | scripts/README.md:150 (`/admin/clusters/{cid}/layout` renders) + README:37 | ✅ shipped |
| 8 | Comparison collage | competitive-landscape context (blog-only, not required) | 🟡 optional |

---

## Where each shot gets used (summary)

| Shot | README hero | Features section | Blog post | Comparison table |
|------|-------------|------------------|-----------|------------------|
| clusters-list.png | ✅ line 12 | — | — | — |
| bucket-detail.png | — | ✅ "Bucket admin" row | — | — |
| key-permission-grid.png | — | ✅ per-bucket permissions | ✅ auth post | — |
| oidc-login.png | — | ✅ OIDC + local password | ✅ auth post | — |
| cross-cluster-buckets.png | — | ✅ Multi-cluster admin | — | — |
| empty-state-welcome.png | — | — | ✅ onboarding | — |
| layout-editor.png | — | ✅ Layout editor (Garage) | — | — |
| comparison-collage.png | — | — | ✅ landscape post | ✅ visual appendix |

---

## If you descope shots 7-8

Shot 7 (layout editor) and Shot 8 (collage) are **polish for v0.5.x followup** if:
- Layout editor hasn't been QA'd yet on staging deploy
- Comparison collage feels like overkill for initial README launch

In that case, capture shots 1-6 first and commit separately. Note the descope in commit body: `docs: screenshot shot list (shots 7-8 v0.5.x followup)`.
