# Developer Certificate of Origin

basement requires every commit to carry a `Signed-off-by:` trailer
attesting to the **Developer Certificate of Origin v1.1** reproduced
below. See [`CONTRIBUTING.md`](../CONTRIBUTING.md) for the project's
contribution terms.

To sign off a commit, use `git commit -s`, which appends:

```
Signed-off-by: Your Name <you@example.com>
```

The name and email must match those used elsewhere in the project
(your `git config user.name` / `user.email`). Anonymous or
pseudonymous sign-offs are not accepted.

If you forget to sign off, amend the commit (`git commit --amend
-s`) or, for a multi-commit branch, run `git rebase --signoff main`
before pushing.

## Developer Certificate of Origin 1.1

```
Developer Certificate of Origin
Version 1.1

Copyright (C) 2004, 2006 The Linux Foundation and its contributors.

Everyone is permitted to copy and distribute verbatim copies of this
license document, but changing it is not allowed.


Developer's Certificate of Origin 1.1

By making a contribution to this project, I certify that:

(a) The contribution was created in whole or in part by me and I
    have the right to submit it under the open source license
    indicated in the file; or

(b) The contribution is based upon previous work that, to the best
    of my knowledge, is covered under an appropriate open source
    license and I have the right under that license to submit that
    work with modifications, whether created in whole or in part
    by me, under the same open source license (unless I am
    permitted to submit under a different license), as indicated
    in the file; or

(c) The contribution was provided directly to me by some other
    person who certified (a), (b) or (c) and I have not modified
    it.

(d) I understand and agree that this project and the contribution
    are public and that a record of the contribution (including all
    personal information I submit with it, including my sign-off) is
    maintained indefinitely and may be redistributed consistent with
    this project or the open source license(s) involved.
```

## Project-specific addendum

basement is licensed under AGPL-3.0 with a commercial dual-licensing
path retained by the project maintainer (see `CONTRIBUTING.md`). By
signing off under the DCO, you are also confirming that you are
comfortable with the maintainer relicensing your contribution as part
of an aggregated commercial license offering — which is consistent
with item (a) / (b) of the DCO above (you have the right to submit
under the indicated open-source license, and the open-source license
in question is AGPL-3.0 alongside the project's dual-licensing
practice).

If you cannot agree to that addendum, please reach out at
`matthew@pq.io` before opening a PR so we can find a workable path.
