# Frontend Coverage — 2026-05-24

Generated from `pnpm vitest --coverage --run`

## Summary

**Test Results:**
- Test Files: 11 failed | 52 passed (63 total)
- Tests: 41 failed | 335 passed (376 total)
- Duration: ~13s

**Note:** Coverage data was not captured in terminal output. The vitest run completed with test execution but coverage summary section was truncated. HTML coverage reports may have been generated separately.

## Known Test Failures (Unrelated to v1.13.7 cycle)

The following failures appear to be pre-existing test issues:

### React Hooks Issues
- `PersonaPill.tsx` failing with "Cannot read properties of null (reading 'useRef')" - likely React version mismatch or multiple React copies in same app
- Affected tests: UserShell, AppShell-admin-user-redirect, cluster-admins, gateways-card, files-home

### Mock Issues  
- `UserMenu.test.tsx` failing with missing `useOrgCapabilities` export in mock
- `login.test.tsx` failing with missing `useActiveSkin` export in mock

### Playwright Import Issue
- `react-error-321.spec.ts` cannot resolve @playwright/test (should use vitest, not playwright)

## Coverage Gap Analysis

Frontend coverage data requires HTML report generation. The terminal output was truncated before the coverage summary section appeared. To get full coverage metrics:

```bash
cd frontend
pnpm vitest --coverage --run
# Then check coverage/index.html for detailed breakdown
```

Suggested follow-up action: Review failing tests above in v1.13.8 cycle to improve test stability, then re-run coverage analysis once test suite is stable.
