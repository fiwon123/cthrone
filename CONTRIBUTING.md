# Contributing

Thanks for wanting to help. This project is small and it needs more kinds of
help than patches — reading the list below is genuinely useful.

## Report an issue

Open an [issue](https://github.com/fiwon123/cthrone/issues/new) with:

- **the version** — `cthrone --version` (or the About tab);
- **the platform** — Linux, Windows, macOS, or the Android app;
- **the transport** — websocket, NATS or Bluetooth, and whether both devices
  are on the same network;
- **what you expected, and what happened** — the exact message or status line
  helps more than a summary;
- **what `cthrone peers` prints**, if the problem is connecting: it shows what
  this device has pinned, which answers most pairing questions.

A screenshot of the window is welcome, and so is the text — tap a message to
copy it, or long-press text to select it.

## Discuss

- **Issues** are for bugs and for concrete proposals.
- **[Discussions](https://github.com/fiwon123/cthrone/discussions)** are for
  questions, ideas that are not yet a plan, and "would this be useful?".

## Help without writing code

This is where the project needs the most help right now. Several claims in the
release notes are **unproven**, because the machine to prove them is not here:

- **the Bluetooth transport on Windows** — it compiles and has never run
  ([#224](https://github.com/fiwon123/cthrone/issues/224));
- **the Android app** — the APK has never been installed on a phone, and the
  radio has no Android binding yet
  ([#155](https://github.com/fiwon123/cthrone/issues/155),
  [#206](https://github.com/fiwon123/cthrone/issues/206));
- **a rendered GUI frame** — the GUI tests run headless, so a human look on a
  real screen is worth something ([#155](https://github.com/fiwon123/cthrone/issues/155)).

Running the app and reporting exactly what happened — including "it worked" —
turns an unproven claim into a proven one. That is more valuable than a patch
today.

Documentation is the other gap: a wrong sentence in the README or the wiki is a
bug, and a shorter, clearer one is a contribution.

## Send code

- Open an issue first, so the change and the reason are recorded together.
- Branch from `dev` as `<type>/<issue>-<slug>` (`fix/42-scan-timeout`), and use
  conventional commits (`feat:`, `fix:`, `docs:`, `test:`, `chore:`, `ci:`).
- Run `make check` before pushing — gofmt, vet, tests, the headless GUI suite
  and the cross-builds. A change that was not verified is not finished.
- One feature per pull request. Say in the body what ran and what did not, and
  do not describe a skipped check as a pass.
- `AGENTS.md` holds the detailed rules (branching, commits, the gate, release
  steps). It is written for the agent, but it is the same bar a human patch is
  held to.

## About the AI

This project is developed with an AI agent doing much of the implementation,
and that is not hidden: the commits say what was verified, the pull requests say
what was not, and a human decides what merges. What that means for you:

- **the review bar is the same** — a patch is judged by its tests and its
  evidence, not by who or what wrote it;
- **"it compiles" is not "it works"** — the repository's rule is that a skipped
  run is not a pass, and that applies to agent-written code first of all;
- **a human is on the other end** — issues, discussions and reviews are read by a
  person, and nothing is merged without one.
