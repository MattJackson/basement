# React Error #321 Reproduction Report

**Cycle:** v1.13.3  
**Date:** May 24, 2026  
**Status:** Investigation in progress

## Overview

React error #321 is a non-specific error code that appears in the console during certain user interactions but has not been captured with a concrete reproduction path. This document tracks investigation findings and potential root causes.

## Error Code Reference

React error #321 corresponds to: **"Failed to perform work on a partially resolved promise"**

This error typically occurs when:
- A component attempts to update state after an unmount
- Async operations resolve after their consumers have been cleaned up
- Race conditions between navigation and data fetching
- Improper cleanup in `useEffect` hooks

## Investigation Methodology

### Automated Route Scanning

A Playwright test (`frontend/react-error-321.spec.ts`) was created to:
1. Navigate to every route in `frontend/src/routes/`
2. Listen for `console.error` events with "321" pattern
3. Capture full stack traces and component stacks
4. Test interactive elements that might trigger the error

### Manual Verification Points

The following high-risk areas were identified for manual testing:
- Skin settings toggle (User Overridable switch)
- Dynamic route transitions (/files/$regionId)
- Admin modal dialogs (OIDC mappings, policy edits)
- Toast notification interactions
- Lazy-loaded components

## Findings

### Current Status

**No concrete reproduction captured.** The error appears intermittently in production but has not been consistently reproduced in local development or test environments.

### Potential Trigger Points

Based on code review and error pattern analysis:

1. **SkinsManager Component** (`frontend/src/routes/admin/system.tsx:1428-1613`)
   - Debounced save effect with 500ms delay
   - State updates after `caps.data` refetch
   - Potential race condition between skin selection and API response

```typescript
// Lines 1467-1487: Debounced save
useEffect(() => {
  const timer = setTimeout(async () => {
    try {
      await client.PATCH("/admin/system", {
        body: {
          activeSkin: defaultSkin,
          userOverridableSkin: userOverridable,
          allowedUserSkins: allowedUserSkins,
        },
      });
      toast.success("Saved");
      queryClient.invalidateQueries({ queryKey: ["org-capabilities"] });
      queryClient.invalidateQueries({ queryKey: ["skin", "active"] });
    } catch (error) {
      toast.error("Failed to save skin settings");
    }
  }, 500);

  return () => clearTimeout(timer); // Cleanup handler present
}, [defaultSkin, userOverridable, allowedUserSkins, queryClient]);
```

2. **Query Refetch Chain** (`useOrgCapabilities` → `invalidateQueries`)
   - Skin changes trigger `queryClient.invalidateQueries()` 
   - Multiple queries may refetch simultaneously
   - Race between invalidation and component unmount

3. **Toast Notifications** (sonner library)
   - Double-toast issue was flagged in v1.13.2 smoke test
   - Toast auto-dismiss with rapid re-trigger could cause cleanup issues

### Code Patterns to Investigate

#### Pattern 1: useEffect Dependency Issues

```typescript
// Risky pattern: missing dependencies
useEffect(() => {
  fetchSkins(); // uses 'caps.data' but not in deps?
}, []); // Should include [caps.data] if dependent
```

**Files to check:**
- `frontend/src/routes/admin/system.tsx` - SkinsManager useEffect (line 1438)
- `frontend/src/shared/api/queries.ts` - query hooks with stale closures

#### Pattern 2: Async Operations Without Cleanup

```typescript
// Risky pattern: async function without abort signal
async function fetchSkins() {
  const data = await client.GET("/api/v1/skins"); // No AbortController
}
```

**Files to check:**
- Any `useEffect` with async API calls
- Custom hooks returning promises without cleanup

#### Pattern 3: State Updates After Unmount

```typescript
// Risky pattern: setState in promise callback
useEffect(() => {
  fetchData().then(data => setData(data)); // Component may unmount before resolve
}, []);
```

**Mitigation patterns to verify:**
- AbortController usage for fetch calls
- `isMounted` ref checks before setState
- Cleanup functions that cancel pending operations

## Recommended Fixes

### Immediate (Low Risk, <15 lines)

If a specific fire site is identified during live testing:

1. **Add AbortController to async fetches:**
```typescript
useEffect(() => {
  const abortController = new AbortController();
  
  async function fetchData() {
    try {
      const data = await client.GET("/api/v1/skins", {
        signal: abortController.signal
      });
      if (!abortController.signal.aborted) {
        setSkins(data);
      }
    } catch (error) {
      if (!(error instanceof DOMException && error.name === 'AbortError')) {
        toast.error("Failed to load skins");
      }
    }
  }

  fetchData();
  
  return () => abortController.abort();
}, []);
```

2. **Fix useEffect dependencies:**
```typescript
// Before (potentially missing deps)
useEffect(() => {
  setDefaultSkin(caps.data?.activeSkin || "basement-default");
}, [caps.data]); // Ensure caps is stable or include all dependencies

// After (with memoization if needed)
const stableCaps = useMemo(() => caps.data, [caps.data]);
useEffect(() => {
  setDefaultSkin(stableCaps?.activeSkin || "basement-default");
}, [stableCaps]);
```

### Medium Priority

1. **Add React StrictMode warnings** during development to catch double-invocation issues
2. **Implement error boundary** around high-risk components with detailed logging
3. **Add query stabilization** using `staleTime` and `gcTime` in React Query config

## Next Steps

### If Error Reproduces During Live Testing

1. **Capture exact URL and action** that triggered the error
2. **Open DevTools Console** and copy full stack trace including:
   - Component stack (e.g., `at SkinsManager at Route`)
   - Call tree leading to error
   - Timestamp and user session ID if available

3. **Update this document** with:
   ```markdown
   ### Fire Site #N: [Brief Description]
   - URL: /path/to/page
   - Trigger: User action (e.g., "Toggled User Overridable switch")
   - Stack Trace: Full error output from console
   - Component Path: Route hierarchy leading to error
   - Reproduction Steps: Exact steps to reproduce consistently
   
   Root Cause Analysis: [If identified]
   
   Fix Applied: [If fixed inline, reference commit]
   ```

### Escalation Criteria

This issue should be escalated if:
- Error occurs on >5% of page views in production
- Error prevents user from completing critical workflows
- Error correlates with data corruption or loss
- Root cause requires multi-file refactoring

## Related Issues

- **Sev-3: Double-toast warnings** (v1.13.2 smoke test) - potential shared root cause
- **Sev-2: WebDAV OPTIONS/PROPFIND failures** - infrastructure issue, unrelated to React errors

## Testing Checklist

Before closing this investigation:

- [ ] Playwright test (`frontend/react-error-321.spec.ts`) runs green in CI
- [ ] Manual testing of all routes completed with console monitoring
- [ ] Interactive elements tested (modals, toggles, form submissions)
- [ ] Network throttling tests performed (slow connections may expose race conditions)
- [ ] Mobile viewport testing completed
- [ ] Error boundary logging verified in production

## References

- [React Error Codes](https://react.dev/reference/react/Component#handling-errors-with-error-boundaries)
- [React Query Cleanup Patterns](https://tanstack.com/query/latest/docs/react/guides/query-functions#aborting-requests-on-unmount)
- [Sonner Toast Documentation](https://sonner.emilkowal.ski/)
