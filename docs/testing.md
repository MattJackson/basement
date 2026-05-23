# Testing

basement runs four layers of tests, each catching a different class
of regression. The order below is roughly the test pyramid — wide
+ fast at the bottom, narrow + slow at the top.

## 1. Unit tests (Go + Vitest)

The widest layer. Every package has `_test.go` (Go) or
`__tests__/*.test.tsx` (frontend) coverage. Run on every commit
locally; gates every PR via CI.

```bash
# Go: race detector + every package
go test -race ./...

# Frontend: vitest single-run + lint + build
cd frontend
pnpm install
pnpm test:run
pnpm lint
pnpm build
```

What this layer catches:

- Driver translation bugs (a malformed S3 response → driver returns
  the wrong shape; a Garage v2 wire-format change).
- API handler shape changes (a route returning the wrong status, a
  missing field on the response body).
- Frontend component regressions (a button missing its handler, a
  form state machine accepting an invalid combination).
- AES-GCM round-trip correctness (encrypt then decrypt yields the
  same plaintext).

The Go unit suite is ~29 packages and ~5 seconds with the race
detector. The frontend suite is ~360 tests and ~10 seconds.
**Both must be green on every commit before review.**

## 2. Docslint (Go)

`internal/docslint/` is a test-only package that asserts the
trust-and-credibility docs + the screenshots gallery stay
internally consistent.

```bash
go test ./internal/docslint/...
```

What this layer catches:

- A README refactor that silently breaks the gallery embed.
- A `SECURITY.md` rewrite that drops the AES-256-GCM claim or the
  48-hour response-SLA promise.
- A `CONTRIBUTING.md` edit that loses the DCO sign-off instructions.
- A future git-rm of any of the 15 v1.10 gallery PNGs (the test
  accepts either `*.png` or `-mocked.png` so a fresh AWS/MinIO
  capture can drop the `-mocked` suffix without test churn).
- An issue-template change that removes one of the four driver
  options from the bug-report form.

Adding a new claim to SECURITY.md or CONTRIBUTING.md? Extend the
test to assert the claim's substring so the claim can't silently
disappear in a future rewrite.

## 3. Feature-coverage smoke (Playwright, against test backends)

`scripts/feature-smoke.ts` walks every product feature A through O
(cluster + key + bucket basics, UserRegions, presign, multipart,
backups, webhooks, service accounts, lifecycle, versioning /
object-lock / SSE stub paths, WebDAV, audit, onboarding) using
real HTTP calls against a real basement deploy. The script targets
**only** test backends — the `feat-smoke-` prefix is enforced on
every destructive verb; the operator's `classe` cluster and
`lsi`/`cheshire` regions are deny-listed and baseline-snapshotted
to catch drift.

```bash
bash scripts/feature-smoke.sh                       # 46 checks + bug report
# or against another deploy:
BASE_URL=https://basement.example.com \
  BUI_USERNAME=alice BUI_PASSWORD=hunter2 \
  bash scripts/feature-smoke.sh
```

Output: pass/fail count per feature + a generated bug report at
`docs/feature-smoke-latest.md` (auto-overwritten on each run; the
companion `docs/feature-smoke-bugs.md` is the curated long-form
report from the v1.11.0.5 cycle).

What this layer catches:

- Cross-cluster dispatch regressions (a per-cluster handler still
  reaching for the global `s.drv` instead of `s.reg.For(ctx, cid)`).
- Driver fix verification on a live backend (BUG02 in v1.11.0.5 —
  Garage v2 key-grant decode).
- Routes that *exist* in the smoke target but never get exercised
  by unit tests.

## 4. Comprehensive UI smoke (Playwright, against live deploy)

`scripts/comprehensive-smoke.ts` is the milestone-gate full-route
walk. Enumerates every route, exercises baseline + mobile + auth
states, takes ~150 screenshots per run, and asserts zero console
errors / page-errors across the walk.

```bash
bash scripts/comprehensive-smoke.sh
```

Per the *senior-smoke-at-minor-releases* doctrine, this runs at
v1.x.0 milestone tags. Patch releases (v1.x.0.N) rely on the
feature-smoke layer above plus targeted unit-test coverage for the
fix.

What this layer catches:

- Hydration regressions in the AppShell.
- Mobile-viewport tap-target regressions.
- Console errors that don't fail an API call but indicate broken
  client-side wiring.
- Render breakage on routes the operator hasn't manually clicked
  through recently.

## 5. Postdeploy UI smoke (Playwright, post-deploy gate)

`scripts/postdeploy-ui-smoke.ts` is the narrow post-deploy check
that runs after every tag push. ~78 checks covering the routes most
likely to break across releases: auth, gallery presence,
documentation files, `/metrics` exposition, first-run wizard.

```bash
bash scripts/postdeploy-ui-smoke.sh
```

The smoke also verifies the [v1.9c] WebDAV-verb passthrough probe
(the bug that bit v1.9 operators when their reverse proxy stripped
PROPFIND).

## CI gates

GitHub Actions runs the following on every PR (v1.11.0.12 added the
Tier 1 quality gates):

- `pnpm build` — frontend builds clean (TypeScript + Vite).
- `pnpm test:run` — vitest passes.
- `pnpm lint` (eslint) + `tsc --noEmit` — frontend lint + typecheck.
- `go test -race ./...` — full Go suite with race detector.
- `go vet` + `golangci-lint` — Go static analysis.
- `gosec` (high-severity floor; `.gosec.yml`) — security audit.
- `govulncheck` — vulnerable transitive deps.
- `gitleaks` — secrets-in-repo scan (block-mode; any leak fails).
- `spectral` — OpenAPI spec lint.
- DCO check — every commit signed off (`Signed-off-by:` trailer).
- SBOM workflow (on tag push only) — `.github/workflows/sbom.yml`
  builds a CycloneDX SBOM with syft and attaches it to the release.
- Trivy (on tag push) — container image CVE scan.
- Lighthouse CI (on `frontend/**` touches) — performance + a11y
  thresholds in `.lighthouserc.json`.

Most gates start in **warn mode** and tighten to block-mode as the
baseline is characterised. See
[`security-audit-baseline.md`](security-audit-baseline.md) for the
current per-gate status + tightening log.

A green PR means every block-mode gate passed and every warn-mode
gate is tracked in the baseline doc. **No `--no-verify` and no
skipped checks.**

Local mirror: `make quality` runs the same gate set against your
working tree.

## What to test in a new cycle

Roughly:

| Change | Add tests at |
|--------|-------------|
| New API handler | `internal/api/*_test.go` (table-driven; assert status + body shape + audit emission) |
| New driver method | `internal/drivers/{name}/*_test.go` (fake HTTP backend; assert verb + body) |
| New frontend component | `frontend/src/.../__tests__/*.test.tsx` (vitest + testing-library) |
| New env var | `internal/config/config_test.go` (load + validation) |
| New doc-level invariant | `internal/docslint/docslint_test.go` (substring/regex assertion) |
| Cross-cutting feature | extend `scripts/feature-smoke.ts` with a new feature block A-O |
| New route | extend `scripts/comprehensive-smoke.ts` route enumeration |

When in doubt: write the unit test first, the smoke check second,
the docslint claim third.

## See also

- [`../CONTRIBUTING.md`](../CONTRIBUTING.md) — local dev loop + DCO
  + style rules.
- [`architecture.md`](architecture.md) — what the code looks like
  underneath the tests.
- [`feature-matrix.md`](feature-matrix.md) — what's actually testable
  per driver (versioning is only meaningful on AWS S3 + MinIO).
