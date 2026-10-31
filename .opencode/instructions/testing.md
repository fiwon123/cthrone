## Testing Requirements

### General

- Tests are mandatory for new features and bug fixes
- Write tests before or alongside implementation
- Mock external services when possible — never call third-party production APIs in tests
- Aim for critical path coverage, not 100% line coverage

### Gates and baselines

A gate is only worth having if it can fail.

- **Do not fix pre-existing failures inside an unrelated PR.** If the suite (or
  `make check`) is already red when you start, your change is not the cause and
  the fix is not the change you set out to make. Say so, and either fix it
  forward on its own issue/branch or record it as a baseline below. Bundling an
  unrelated repair into your PR makes "is my change correct?" unanswerable from
  the diff.
- **Record a known-failing baseline.** When you inherit a red suite, write down
  which tests fail and why (a tracked issue, a TODO, the failing assertion) so
  the next person does not re-investigate the same failures. A baseline turns
  "the suite is red, I don't know why" into "these N are known-red".
- **A gate that cannot fail is worse than no gate.** A test target that collects
  zero tests, a CI job that always skips, or an assertion with no possible
  failure manufactures false confidence: it reads green and proves nothing. If a
  gate reports success without executing anything, treat it as **unverified**,
  not as passed — a skipped run is not a passed run.

### The current state of this suite — read this before claiming coverage

**Every package has a test file**: #85/#86 closed the last gap, so all 27
packages in the module carry tests and `go test ./...` prints no
`? package [no test files]` line.

- **Covered (27)**: `main`, `cmd` (root), `cmd/assets`, `cmd/connect`,
  `cmd/exec`, `cmd/host`, `cmd/peers`, `cmd/recv`, `cmd/scan`, `cmd/ui`,
  `internal/auth`, `internal/chat`, `internal/core/nats`,
  `internal/core/websocket` (scanner included), `internal/data/app`,
  `internal/discovery`, `internal/e2e`, `internal/exec`,
  `internal/handlers/nats`, `internal/handlers/websocket`, `internal/pairing`,
  `internal/session`, `internal/shutdown`, `internal/wire`, `mobile`, `ui` (its
  non-GUI settings logic), `ui/widgets` (the selectable-text helpers), and the
  gui-tagged widget tree (headless).

So a green `make check` proves the code is `gofmt`-clean, `go vet` found
nothing, it compiles, every package's tests passed, and the gui-tagged widget
tree's tests passed under Fyne's software driver.

**A test file is not depth.** Several newer files hold one test function
(`cmd/assets`, `cmd/ui`, `internal/data/app`, `mobile`). A gui-tagged widget
change is now executed, not only type-checked: `make check` and the CI `Test
the GUI headless` step run
`CGO_ENABLED=0 go test -tags "gui ci" ./ui/... ./cmd/ui/...`. The `ci` tag
swaps Fyne's GL driver for the software one, so no GTK/X11/GL is needed. A
native rendered frame and the APK on a device are still unverified (#155).
Report what executed; "27/27 packages have tests" is not "the behaviour is
covered".

### Go conventions

- Test file: `<file>_test.go`, next to the code it tests, same package (or
  `<package>_test` when the test is exercising the public surface only)
- Test function: `func Test<Thing>(<t *testing.T>)`
- Use the standard library only — no assertion framework unless the project
  already depends on one
- Table-driven tests for anything with more than one case:

  ```go
  func TestParse(t *testing.T) {
      tests := []struct {
          name    string
          input   string
          want    int
          wantErr bool
      }{
          {"empty", "", 0, true},
          {"one", "1", 1, false},
      }
      for _, tt := range tests {
          t.Run(tt.name, func(t *testing.T) {
              got, err := parse(tt.input)
              if (err != nil) != tt.wantErr {
                  t.Fatalf("parse(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
              }
              if got != tt.want {
                  t.Errorf("parse(%q) = %d, want %d", tt.input, got, tt.want)
              }
          })
      }
  }
  ```

- `t.TempDir()` for anything that touches the filesystem — never write into the
  working tree from a test
- `t.Setenv()` for environment variables (it restores the previous value and
  refuses to run in parallel)
- `httptest.NewServer()` for HTTP handlers; `httptest.NewRecorder()` when you
  only need the response
- Assert on error identity with `errors.Is` / `errors.As`, not on the message
  text — a message is not part of the contract
- `t.Helper()` in test helpers, so a failure points at the caller
- Never let a test reach the network, and never let it depend on wall-clock
  time or a random seed unless it controls both

### GUI tests

The Fyne GUI is behind the `gui` build tag, so plain `go test ./...` never
compiles it. Test it headless instead:

```bash
CGO_ENABLED=0 go test -tags "gui ci" ./ui/... ./cmd/ui/...
```

The `ci` tag swaps Fyne's GL driver for the software one, so this needs no
GTK/X11/GL. `make check` and the CI `Test the GUI headless` step run it, and
`make test-gui` runs it alone. Build the tree with `test.NewApp()` and a
`t.TempDir()` configuration home — never the real one. Assert widget state (a
row's text, an image's `Visible`/`Aspect`), not pixels.

### Database tests (when the project has a database)

- A test that truncates, drops, or rewrites tables acts on whatever
  `DATABASE_URL` points at. Guard it:
  - before any destructive operation, assert the connected database's *name*
    matches the test database (e.g. `SELECT current_database()` equals
    `<project>_test`) and abort otherwise;
  - never point the destructive suite at a connection string that production also
    uses without that assertion in front of it;
  - prefer a dedicated test database whose name cannot be confused for the real
    one.

This is the difference between a mistyped environment variable failing a run and
it wiping a real database.

### Before committing

1. Run `make check` — gofmt, vet, test, build
2. If the change touches a package that has no tests, say so in the PR body
3. Verify no regressions: if the suite was already red, confirm your change did
   not add to it

### Visual checks

The GUI renders screens, but this environment has no toolkit to draw them: the
headless test above asserts widget state, not pixels. A change to the native
layout still needs a human look on a machine with GTK/X11/GL — or a phone for
the APK — and that is recorded in the PR body rather than implied by a green
`make check`. There is no `visual` subagent in this repository.
