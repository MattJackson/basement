<!--
Thanks for the PR. Please fill out each section. Delete the HTML
comments before submitting.
-->

## Summary

<!--
1-3 sentences. What does this PR change and why? Operator-facing
wording is best — readers want to know what they'll see, not
which file you touched.
-->

## Test plan

<!--
Check the boxes that apply. PRs without local test runs may be
asked to wait for CI before review.
-->

- [ ] `go test -race ./...` passes locally
- [ ] `cd frontend && pnpm build && pnpm test:run && pnpm lint` passes locally
- [ ] Added or updated unit tests covering the change
- [ ] Manually exercised the affected UI flow / API in a dev deploy
- [ ] Updated `CHANGELOG.md` (for operator-visible changes)
- [ ] Updated `docs/release-notes/` (for minor-version cycles)

## Linked issues

<!--
Closes #123 / Refs #456 / etc. Required if there's a corresponding
issue.
-->

## DCO sign-off

By submitting this PR I confirm that **every commit carries a
`Signed-off-by:` trailer** attesting to the Developer Certificate
of Origin v1.1 (`git commit -s`), and I have read
[`CONTRIBUTING.md`](../CONTRIBUTING.md), including the commercial
dual-licensing addendum in [`.github/DCO.md`](./DCO.md).

If any commit is missing a sign-off, fix it with:

```
git commit --amend -s
# or for a multi-commit branch:
git rebase --signoff main
```

then `git push --force-with-lease`.

## Reviewer notes

<!--
Anything the reviewer should pay attention to. Tricky edge cases,
deliberate scope cuts, follow-up issues you plan to file, etc.
-->
