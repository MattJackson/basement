# basement screenshots

Captured screenshots used by the project README, release notes, and
launch materials. Organized by release.

Capture script: `scripts/capture-v1.10-screenshots.ts` (Playwright,
runs against a live deploy + falls back to mocked component renders
where features aren't exercisable on the target deploy).

## v1.10 gallery (15 shots)

Last updated: 2026-05-23 against `https://basement.pq.io` (Garage v1
single-region deploy). Re-shoot with:

```bash
node scripts/capture-v1.10-screenshots.ts
# or against another deploy:
BASE_URL=https://basement.example.com \
  BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
  node scripts/capture-v1.10-screenshots.ts
```

Files live under `v1.10/`. Shots ending in `-mocked.png` are
Playwright-driven static-HTML renders that approximate components
which cannot render against this deploy (e.g. `ObjectVersionsPanel`
requires versioning support; Garage backends advertise versioning as
unsupported). Mocked shots embed an explicit disclaimer below the
content so they're transparent in documentation use.

| #  | File                                       | What it shows                                          | Source       |
|----|--------------------------------------------|--------------------------------------------------------|--------------|
| 01 | `01-clusters-list.png`                     | `/admin/clusters` with Garage v1 + v2 clusters         | live         |
| 02 | `02-bucket-browser-desktop.png`            | bucket browser at 1440x900                             | live         |
| 03 | `03-bucket-browser-mobile.png`             | bucket browser at 375x667 (mobile)                     | live         |
| 04 | `04-bucket-versioning-section.png`         | `VersioningSection` card (Garage: Unsupported branch)  | live         |
| 05 | `05-bucket-object-lock-section.png`        | `ObjectLockSection` card (Garage: Unsupported branch)  | live         |
| 06 | `06-bucket-encryption-section.png`         | `EncryptionSection` card (Garage: Unsupported branch)  | live         |
| 07 | `07-object-versions-panel-mocked.png`      | `ObjectVersionsPanel` per-version actions              | mock         |
| 08 | `08-federation-detail-mocked.png`          | `/files/federated-buckets/$id` replica health table    | mock         |
| 09 | `09-federation-wizard-step3-mocked.png`    | federation wizard step 3 (policy)                      | mock         |
| 10 | `10-backup-detail-snapshots-mocked.png`    | `/files/backups/$id` snapshot table + restore links    | mock         |
| 11 | `11-service-accounts-list.png`             | `/admin/service-accounts` list                         | live         |
| 12 | `12-mcp-config-dialog.png`                 | `McpConfigSection` config.yaml + Claude/Cursor JSON    | live         |
| 13 | `13-admin-gateways-card.png`               | `/admin/system` Gateways card (registry-driven)        | live         |
| 14 | `14-policy-matrix.png`                     | `/admin/policies` matrix + simulator                   | live         |
| 15 | `15-audit-log-filtered.png`                | `/admin/audit` filtered view + pagination              | live         |

## Per-shot notes

### live shots

The five "Unsupported" shots (04, 05, 06 and the Garage-v1 perspective
in 02, 03) are deliberate: this deploy is Garage-only, and Garage's
content-addressed block store conflicts with versioned overwrites +
S3-style object lock + per-bucket SSE. The UI renders a graceful
"Unsupported" branch instead of pretending. AWS S3 + MinIO deploys
render the full feature surface; re-shoot against one of those to
populate the non-mocked counterparts.

### mocked shots

Mocks render a single self-contained `<div data-testid="mock-root">`
via a static HTML template served from a `data:text/html;...` URL,
then crop the screenshot to that element. The HTML is hand-rolled to
match production copy + layout closely enough for documentation
purposes; the bottom of each mocked shot includes an explicit "Mock
render &mdash; ..." footer that names the production component +
explains why a live capture wasn't possible. No images are doctored
or recoloured. Source: `mockHtmlFor()` in
`scripts/capture-v1.10-screenshots.ts`.

## Older shots

`SHOTLIST.md` is the v0.5.0 shot list. Pre-v1.10 capture flow was
operator-driven (no automated capture script); the captured shots
were never committed to git. The v1.11.0e cycle introduced the
Playwright capture + the mocked fallback so re-shoots are repeatable
across deploys.
