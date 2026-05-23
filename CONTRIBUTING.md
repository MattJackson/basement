# Contributing to basement

basement is an AGPL-3.0 multi-backend storage control plane. Patches,
bug reports, and integration drivers are welcome.

## License + contributor terms

basement is licensed under the **GNU Affero General Public License,
version 3** ([`LICENSE`](./LICENSE)). By submitting a pull request,
patch, or any other contribution, you agree that **your contribution
is licensed to the project and its users under AGPL-3.0**, in the
same terms as the rest of the codebase.

### Commercial dual-licensing path

The project maintainer (Matthew Jackson, `matthew@pq.io`) reserves
the right to grant **commercial licenses** to basement for
proprietary embedding, hosted SaaS deployments, or other use cases
the AGPL's copyleft does not suit. This requires the maintainer to
hold (or be permitted to relicense) the copyright on every
contribution that ships in the binary.

To make that workable without copyright assignment, **every
contribution must be signed off under the Developer Certificate of
Origin (DCO) v1.1**. The DCO is a lightweight, well-understood
attestation that says:

> I have the right to submit this code, and I'm contributing it
> under the project's open-source license. I am also fine with the
> project author relicensing my contribution under different terms
> as the project deems necessary.

The full DCO text is in [`.github/DCO.md`](./.github/DCO.md).

In practice, DCO sign-off means adding one trailer to every commit:

```
Signed-off-by: Your Name <you@example.com>
```

`git commit -s` adds this automatically. If you forget, the CI
check will fail and you can fix it with `git commit --amend -s`
(or `git rebase --signoff main` for a multi-commit branch).

Contributions without a DCO sign-off cannot be merged. We do not
require copyright assignment.

## Developing locally

basement is a Go backend (`internal/`, `cmd/`) with an embedded
React + Vite frontend (`frontend/`). The frontend builds into
`internal/web/dist/` which the Go binary serves via `embed.FS`.

### Prerequisites

- **Go 1.25+** (see `go.mod`).
- **Node.js 22+** and **pnpm 11+** (see
  `frontend/package.json#packageManager`).
- **Docker** if you want to run the example multi-cluster
  development stack.

### Clone + build

```bash
git clone https://github.com/mattjackson/basement
cd basement

# Frontend: install deps, build, drop dist/ into internal/web/dist/
cd frontend
pnpm install
pnpm build

# Backend: compile + run unit tests
cd ..
go build ./...
go test -race ./...
```

### Run the dev stack

```bash
cd deploy
cp .env.example .env       # edit values, especially BASEMENT_JWT_SECRET
docker compose -f docker-compose.example.yml up -d
# basement on https://localhost (Caddy fronts the Go binary on :8080)
```

This brings up a Garage container, a MinIO container, and an AWS S3
connection (using env-supplied credentials), so you can exercise
all four drivers from one UI. Sign in with the env-seeded admin
(default `admin / changeme`).

### Iterating on the frontend

```bash
cd frontend
pnpm dev            # vite on :5173, proxies /api/* to :8080
pnpm test:run       # vitest single-run
pnpm lint           # eslint
```

### Iterating on the backend

```bash
go test -race ./...                 # all unit tests with race detector
go test -race ./internal/api/...    # subset
gofmt -l . | tee /dev/stderr        # any output = unformatted files
```

## Testing — the basement test pyramid

basement maintains a layered test strategy so every change has a
fast feedback loop AND eventually a deep one. From fastest to most
exhaustive:

1. **Unit tests** — `go test -race ./...` and
   `pnpm -C frontend test:run`. Sub-second per package; this is what
   pre-commit and CI run on every push. Add new tests under
   `_test.go` (Go) or `__tests__/` (vitest).

2. **Integration tests (real Garage containers)** — `make integration`
   runs the driver + federation suites against real Garage v1
   (`dxflrs/garage:v1.0.1`) and v2 (`dxflrs/garage:v2.0.0`)
   containers spun up via
   [`testcontainers-go`](https://github.com/testcontainers/testcontainers-go).
   The suites are gated behind the `integration` Go build tag so
   `go test ./...` stays Docker-free. They are the regression net
   for the v1.11.0.1 / v1.11.0.2 / v1.11.0.4 / v1.11.0.5 BUG02 bug
   classes — each was a real-Garage interaction the unit suite
   couldn't have caught. CI runs the same suites via
   `.github/workflows/integration.yml` on every PR + every push to
   `main` that touches driver or federation code paths.

   ```bash
   make integration                                                   # all (Garage v1, v2, federation)
   go test -tags=integration -race -v ./internal/drivers/garage/...    # Garage v2 driver
   go test -tags=integration -race -v ./internal/drivers/garage_v1/... # Garage v1 driver
   go test -tags=integration -race -v ./internal/federation/...        # federation E2E (2x v2)
   ```

   Requires a Docker daemon (or Docker-compatible runtime like
   colima, Rancher Desktop, Podman with docker.sock).

3. **Feature smoke** — `bash scripts/feature-smoke.sh`. Runs
   the per-feature functional smoke against the 10x Garage v2 test
   backend (10.1.7.11:38xx). Hard safety gate: every destructive op
   asserts the target cluster label starts with `garage-v2-test-`.

4. **UI smoke** — `pnpm -C frontend smoke` (curated, ~70 checks) and
   `pnpm -C frontend smoke:full` (comprehensive walk, every route +
   axe-core a11y pass). Both target the live deploy
   (`basement.pq.io`) using the matthew/password credentials.

5. **Fuzz** — `go test -fuzz=FuzzMatchFilter -fuzztime=30s
   ./internal/audit/...` (cycle v1.11.0.12 starter). Pure-function
   fuzzers live alongside the code they exercise as `*_fuzz_test.go`.
   When you add a fuzz target, add it to the `Makefile` fuzz-*
   targets so it's discoverable.

6. **Quality gates** — `make quality` runs lint + vet + test +
   gosec + govulncheck locally. CI runs the same gates per
   `.github/workflows/quality.yml`. Tightening from warn-mode to
   block-mode is per-gate and tracked in
   `docs/security-audit-baseline.md`.

7. **Pre-commit hooks** — install once with `pre-commit install`.
   Hooks run gitleaks + whitespace fixers + gofmt on every commit,
   and `go test -race ./...` on every push (slow, push-stage only).

## Code style

- **Go**: `gofmt` (any unformatted file fails review).
  `golangci-lint run ./...` should be clean for the directories
  you touched. Standard-library idioms; avoid heavyweight
  dependencies.
- **TypeScript / React**: `prettier` (default config) +
  `eslint` (the project's `eslint.config.js`). Functional
  components, hooks, no class components. `@tanstack/react-router`
  for routing, `@tanstack/react-query` for server state.
- **Commit messages**: imperative present tense
  ("add foo" not "added foo"). The project uses informal
  `vX.Y.ZN` tag suffixes for release-cycle increments (see
  `CHANGELOG.md` for the pattern).

## Pull-request process

1. **Branch** from `main`. Name it descriptively (`feat/`, `fix/`,
   `docs/` prefixes are fine but not required).
2. **Write tests** for the change. Frontend changes go under
   `frontend/src/**/__tests__/` (vitest). Go changes go in
   `_test.go` files next to the code (`go test -race ./...`).
3. **Run the full test suite** locally:
   ```bash
   cd frontend && pnpm build && pnpm test:run && pnpm lint
   cd .. && go test -race ./...
   ```
   PRs must be green.
4. **Sign off** every commit (`git commit -s`).
5. **Open a PR** with a clear description: what changed, why, and
   what to test. Link the issue if there is one.
6. A reviewer (the maintainer for now) will respond within a few
   days. Larger changes may need design discussion before code
   lands — for those, please open an issue first or start a
   discussion.

## Issues + discussions

- **Bug reports**, **feature requests**, and **questions** all have
  GitHub Issue Forms under `.github/ISSUE_TEMPLATE/`. Please use
  them — they prompt for the context reviewers need.
- **Security reports** go to `matthew@pq.io`, not the public
  issue tracker. See [`SECURITY.md`](./SECURITY.md).
- Blank issues are disabled (`.github/ISSUE_TEMPLATE/config.yml`)
  so reports land in the right template.

## Driver contributions

basement supports four backends today (Garage v1, Garage v2, AWS
S3, MinIO / OpenMaxIO). New drivers are welcome. The driver
interface lives in `internal/driver/driver.go`; the
[driver-parity doctrine](docs/release-notes/) means a new driver
must implement the capability matrix honestly — advertise
"unsupported" via `Driver.<Feature>Available() = false` rather
than faking it, and the UI will render graceful notices.

A new driver PR should land alongside:

- Unit tests in `internal/drivers/<name>/*_test.go`.
- Capability-flag entries.
- A line in the README features table.

## What "ready to merge" looks like

- All tests pass (`go test -race ./...` and `pnpm build` +
  `pnpm test:run` + `pnpm lint`).
- Every commit is signed off (DCO).
- Operator-visible changes are documented in `CHANGELOG.md` and
  (for minors) `docs/release-notes/`.
- No new top-level dependencies without discussion.
- No `Co-Authored-By:` trailers on commits.

## Code of conduct

Be kind, assume good faith, focus on the code rather than the
contributor. No formal CoC document yet; if a situation calls for
one we will adopt the Contributor Covenant.

## Contact

- GitHub issues + discussions: `https://github.com/mattjackson/basement`
- Email: `matthew@pq.io`
- Commercial licensing: `matthew@pq.io`
