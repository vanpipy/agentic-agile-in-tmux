# Mutation Testing for `awp`

> **Status (2026-07-02):** Baseline complete; per-package runs show **100%
> efficacy** on covered code. Full-module runs hit a gremlins bug (issue
> #272) that over-counts LIVED mutants. This doc explains the bug, the
> workaround, and the workflow.

## Goal

Apply mutation testing as a *learning instrument* and *quality probe* for
`awp`'s test suite. Mutation testing mutates the source code in small ways
and checks whether the existing tests catch the change. A test suite that
lets a mutation *live* (i.e. tests still pass after the source change) is
silently weak.

This is exploratory work from the ticket "awp — mutable test":
1. **A baseline number.** How many of `awp`'s ~788 runnable mutants live?
2. **A repeat recipe.** A config + per-package workflow so future
   contributors can run mutation testing on their PRs.

## Tooling

Pinned: [`go-gremlins/gremlins`](https://github.com/go-gremlins/gremlins) `v0.6.0`.

Why gremlins over `avito-tech/go-mutesting`:

| Concern | Gremlins | go-mutesting |
|---|---|---|
| Coverage-aware (skip uncovered) | yes | no |
| `--diff` (mutate only changes vs git ref) | yes | no |
| Adaptive `--timeout-coefficient` | yes | fixed `MUTATE_TIMEOUT` |
| CI threshold gates | yes | no |
| Parallelism (`--workers`) | yes | no |
| Mutator breadth | 11 | 19 (more — branch/case, statement/remove) |

`awp`'s module is moderate-sized (~10K LOC, 357 tests). Gremlins is
purpose-built for that scale.

## ⚠️ Known gremlins bug — full-module runs are unreliable

**Gremlins issue #272** — *"Integration mode (-i) reports mutants as LIVED
when they should be KILLED"*. Root causes:

1. **Test cache** — `getTestArgs` in `internal/engine/executor.go` doesn't
   pass `-count=1`. In multi-package runs Go's test cache can return cached
   `ok` results from a previous clean run.
2. **Go 1.26 exit-code bug** — `go test -failfast ./...` with
   `t.Parallel()` panic returns exit 0 instead of 1.

**Symptom in awp:** full-module `gremlins unleash` reports `Killed: 86,
Lived: 702` (10.91 % efficacy). Per-package `gremlins unleash internal/<pkg>`
on the same source files reports `Killed: 636, Lived: 0` (100 % efficacy).

**Conclusion:** gremlins' full-module mode is broken on this codebase.
**Use per-package runs as the source of truth.**

A patch exists upstream but is not in v0.6.0. Tracking:
<https://github.com/go-gremlins/gremlins/issues/272>.

## Setup

Binary: `$(go env GOPATH)/bin/gremlins`. Verified checksum at install time.

Configuration: `.gremlins.yaml` at the repo root.

## Configuration

Two configs ship with this repo:

| File | Mutators enabled | Purpose |
|---|---|---|
| `.gremlins.yaml` | 5 (default set) | Fast feedback — arithmetic-base, conditionals-{boundary,negation}, increment-decrement, invert-negatives |
| `.gremlins-all.yaml` | 11 (all) | Deep audit — also invert-{assignments,bitwise,bwassign,logical,loopctrl}, remove-self-assignments |

Both share these unleash defaults:

```yaml
silent: false                    # human-readable summary
unleash:
  tags: ""                       # no build tags; unit tests only
  integration: false             # gremlins bug #272 — disabled for now
  threshold: {efficacy: 0, mutant-coverage: 0}  # no gate yet
  timeout-coefficient: 5         # PTY tests use time.Sleep waits
  test-cpu: 1                    # deterministic
  workers: 1                     # debuggable; parallelise later if needed
  exclude-files: ['.*_test\.go$'] # defensive
```

## Workflow

### Baseline (per-package, source of truth)

```sh
for pkg in $(go list ./... | grep -v 'test\|e2e'); do
    echo "=== $pkg ==="
    gremlins --config .gremlins-all.yaml unleash "$pkg" 2>&1 | tail -5
done
```

Runtime: ~3 minutes total across all packages.

### Diff-mode iteration

```sh
gremlins --config .gremlins-all.yaml unleash --diff=origin/main
```

Mutates only what changed vs `origin/main`. Use after each strengthening
commit to confirm no new LIVED mutants were introduced.

### Per-package focus

```sh
gremlins --config .gremlins-all.yaml unleash internal/board
```

## TDD loop for "kill a mutant"

Per AGENTS.md §2.2 (TDD) + §2.1 (CORRECT-7):

1. **RED** — Write a new test that would FAIL under the mutation but PASS on
   the original code. The mutation is documented in the test's commit
   message.
2. **GREEN** — Run the test on the unchanged source; confirm pass.
3. **Manual RED verification** — Apply the mutation by hand (e.g.
   `sed -i 's/foo == ""/foo != ""/' file.go`), run the test, confirm it
   fails. This proves the test would catch the gremlins mutation.
4. **Restore** + **REFACTOR** — Tighten the test; ensure CORRECT-7
   (Conformance / Ordering / Range / Reference / Existence / Cardinality
   / Time).

The commit message links the mutant to the test:

```
test(pi): kill NOT COVERED CONDITIONALS_NEGATION at session.go:292:26

Mutation: == "" → != "" in parseSessionInfo's first-wins guard.

Why NOT COVERED: existing tests only exercise the case where
ModelProvider is empty when a model_change entry is encountered.
No test wrote a JSONL with TWO model_change entries, so the
"second-wins" branch was untested.

Fix: TestParseSessionInfo_ModelChange_FirstWins writes two
model_change entries and asserts on the first winning. Under the
mutation, the test fails (ModelProvider = "" instead of "anthropic").
```

## Reporting

Each run prints a summary on stderr. JSON output via `--output=FILE.json`
is supported but **not reliable** for full-module runs due to issue #272.
Per-package JSON is reliable.

## File layout

```
.gremlins.yaml                 # default-mutator config (5)
.gremlins-all.yaml             # all-mutator config (11)
MUTATION_TESTING.md            # this design note
MUTATION_TESTING_RESULTS.md    # per-run baseline numbers + commentary
mutation-*.json                # gitignored
```

## What is **not** in scope

- **CI gate.** Gremlins supports thresholds, but adding a CI gate is its own
  decision. This ticket only adds the tooling.
- **Modifying production code to make mutants die.** The point is to find
  *test* weaknesses. A `LIVED` mutant that reveals a real semantic gap
  should be killed by a better test, not by changing the production
  behavior.

## Pre-flight checklist (before commit)

Per AGENTS.md §6:

```sh
go build -o awp .                # 0 errors
go vet ./...                     # 0 warnings
go test ./...                    # all pass
go test -race ./internal/pi ./internal/terminal ./internal/ui
```

Plus:

```sh
# Per-package mutation test on the changed package(s)
gremlins --config .gremlins-all.yaml unleash <changed-pkg>
```