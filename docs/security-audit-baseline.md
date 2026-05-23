# Security audit baseline

Established cycle **v1.11.0.12** (2026-05-23).

This file is the operator-facing log of what the Tier 1 CI quality
gates surface and what we have decided to do about each finding. The
gates run in **warn mode** (continue-on-error) for their first cycle
so the baseline can be characterised; subsequent cycles tighten each
gate to block-mode as the underlying issue is fixed or formally
suppressed.

## Gates + status

| Gate | Tool | Mode | Notes |
| --- | --- | --- | --- |
| `go-lint` | golangci-lint | warn | Baseline scan pending; tighten after first clean run. |
| `go-vet` | `go vet` | block | Currently clean. Block-mode from day 1. |
| `go-security` | gosec | warn | Configuration in `.gosec.yml`. Severity filter set to high+. |
| `go-vulns` | govulncheck | warn | Tracks vulnerable transitive deps in the standard tier. |
| `secrets-scan` | gitleaks | block | Hard gate; any leak fails the run. |
| `openapi-lint` | spectral | warn | Spec audit lands as a follow-up cycle. |
| `frontend-lint` | eslint | warn | Baseline: 188 errors, 7 warnings. Mostly `react-hooks/set-state-in-effect` (new rule in eslint-plugin-react-hooks v7). Triage + fix in a frontend-lint-cleanup follow-up cycle. |
| `frontend-typecheck` | tsc | warn | `pnpm build` runs `tsc -b` first (production-gated). The `--noEmit` job is the explicit type-only check; flips to block-mode once baseline is verified. |
| `trivy` (release) | Trivy | warn | First release scan baselines image CVEs; tighten next cycle. |
| `lighthouse` | Lighthouse CI | warn | Performance/a11y thresholds in `.lighthouserc.json`. |

## Known findings (to be triaged in follow-up cycles)

### eslint: 188 errors, 7 warnings (frontend baseline at v1.11.0.12)

- **Tool:** `pnpm -C frontend lint`
- **Total:** 195 problems across the frontend source tree.
- **Dominant rule:** `react-hooks/set-state-in-effect` —
  eslint-plugin-react-hooks v7 added this lint, which flags
  `setState(...)` calls inside `useEffect` bodies. Many of the hits
  are legitimate-but-debatable patterns (sync local state to fetched
  data); a triage pass should split them into "refactor to derived
  state" vs "intentional, suppress with rationale" buckets.
- **Decision:** warn-mode at the gate. A dedicated cleanup cycle
  (frontend-lint-cleanup, post-v1.11.0.12) batches the fix or
  documents per-line `// eslint-disable-line` justifications, then
  flips the gate to block-mode here.
- **Status:** open — tracked as cycle backlog.

_Other gates' findings get populated as each runs for the first
time on the live CI infrastructure. The cycle that characterises a
gate is responsible for filling in:_

- Finding ID / rule
- Affected file(s)
- Triage decision: fix / suppress (with rationale) / accept (with
  rationale and re-review date)

Template entry:

```
### gosec G401 — Use of weak cryptographic primitive

- **File:** internal/foo/bar.go:42
- **Rule:** G401 (CWE-327)
- **Decision:** fix — replace with crypto/aes
- **Cycle:** v1.11.0.8
- **Status:** open
```

## Suppression policy

- **Rule-level global suppressions** live in `.gosec.yml`. Each must
  have a code comment explaining WHY the rule is wrong-for-basement
  rather than wrong-in-general.
- **Per-line `// #nosec` comments** are allowed for one-off cases
  but MUST include a justification trailer:
  `// #nosec G304 -- path is operator-config not user-input`.
- **Trivy ignores** live in `.trivyignore` (does not yet exist;
  added when the first false positive forces it). Every ignore is
  dated + reviewed every 90 days.

## Updating this file

This file is part of the gate-tightening contract: a follow-up cycle
that flips a gate from warn to block MUST update the table above and
record the closure of any findings that the gate now blocks on.
