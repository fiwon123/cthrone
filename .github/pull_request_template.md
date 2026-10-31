<!--
Thanks for the pull request. Keep the sections below — they are read at the
moment a claim is written, which is the only moment it can be checked.

Delete this comment block before you open the PR.
-->

Closes #

## What

<!-- What changed, in one or two sentences, and which files or packages carry it. -->

## Why

<!-- The problem this solves. Link the issue's reasoning rather than restating it. -->

## Verification

<!--
What actually ran, and what it proved. Be specific: a command and its exit
status is evidence; "tests pass" is a claim.

State the gate as it is, not more:

- `make check` covers gofmt, vet, build, the packages that HAVE tests, and the
  gui-tagged widget tree headless (`-tags "gui ci"`, Fyne's software driver).
- A package printed as `[no test files]` was NOT exercised — say so.
- The headless test asserts widget state, not pixels; a native link (CGO,
  GTK3/X11) and a rendered frame still need CI or a machine with the toolkit.
- Workflows are not covered by CI. A `release.yml` change is unverified until a
  run executes the job that exercises it.
- A skipped job, a run that never happened, or an empty test target is
  **unverified**, not passed.
-->

- [ ] `make check` — exit status:
- Untested packages touched (from the `[no test files]` list):
- Anything else that was run, and what it showed:

## Notes

<!--
- Follow-ups deliberately left out of scope, and the issue that tracks them.
- Known caveats a reviewer should not have to rediscover.
- Anything in the diff that looks wrong but is intentional, and why.
-->
