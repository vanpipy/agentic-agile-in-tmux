# Mutation Testing for `awp`

> **Status (2026-07-02):** Initial baseline in progress on branch `task/awp-mutable-test`.
> This document is the working plan; numbers are updated as runs land.

## Goal

Apply mutation testing as a *learning instrument* and *quality probe* for `awp`'s
test suite. Mutation testing mutates the source code in small ways and checks
whether the existing tests catch the change. A test suite that lets a mutation
*live* (i.e. tests still pass after the source change) is silently weak.

This is exploratory: the ticket "awp — mutable test" asked to learn the
repository by trying the technique. Two outputs are wanted:

1. **A baseline number.** How many of `awp`'s ~788 runnable mutants live? That
   quantifies today's test quality on the *covered* code.
2. **A repeat recipe.** A `make mutation` target + `.gremlins.yaml` so future
   contributors can run mutation testing on their PRs.

Strengthening the weak tests is the main deliverable.

## Tooling

Pinned: [`go-gremlins/gremlins`](https://github.com/go-gremlins/gremlins) `v0.6.0`.

Why gremlins over `avito-tech/go-mutesting` (the alternative):

| Concern | Gremlins | go-mutesting |
|---|---|---|
| Coverage-aware (skip uncovered) | yes | no — must mutate everything |
| `--diff` (mutate only changes vs git ref) | yes | no |
| Adaptive `--timeout-coefficient` | yes | fixed `MUTATE_TIMEOUT` |
| CI threshold gates (`--threshold-efficacy`) | yes | no |
| Parallelism (`--workers`) | yes | no |
| Mutator breadth | 11 | 19 (more — branch/case, statement/remove) |

`awp`'s module is moderate-sized (10 packages, ~10K LOC, 357 tests, 68%
coverage). Gremlins is purpose-built for that scale. Diff-mode is the deciding
factor for this branch workflow.

## Setup

Binary is installed in `$(go env GOPATH)/bin` (verified at install time via
checksums). Configuration lives in `.gremlins.yaml` at the module root.

## Configuration

See `.gremlins.yaml` at the repo root. Defaults plus:

- `silent: false` — we want a human-readable summary, not just a JSON blob.
- `unleash.timeout-coefficient: 5` — `internal/terminal/pane_test.go` uses
  `time.Sleep` waits for PTY output; mutations that delete a sleep would
  otherwise time out under fixed timeouts.
- `unleash.test-cpu: 1` — single-CPU test runs are deterministic and faster.
- `unleash.exclude-files: ['.*_test\.go$']` — defensive (gremlins already
  skips tests by default; this makes intent explicit).

## Workflow

### Baseline

```sh
gremlins unleash --output=mutation-baseline.json
```

Result snapshot (initial run, 2026-07-02, dry-run only):

- Runnable: **788**
- Not covered: **1348**
- Mutant coverage: **36.89 %**

This means `awp`'s tests cover ~37 % of mutation sites. The 63 % gap is
expected — the TUI/PTY layer has many rendering branches that aren't exercised
by unit tests.

### Iterative kill

```sh
gremlins unleash --diff=origin/main --output=mutation-diff.json
```

After each strengthening commit, the diff shrinks. Repeat until `--diff` is
empty.

### Per-package focus

For deep dives on one package:

```sh
gremlins unleash ./internal/board/...
```

## TDD loop for "kill a mutant"

Per AGENTS.md §2.2 (TDD) + §2.1 (CORRECT-7), each LIVED mutant is killed via:

1. **RED** — Write a new test that would FAIL under the mutation but PASS on
   the original code. The mutation is documented in the test's commit message
   (`kill(mutant): …`).
2. **GREEN** — Run the test; confirm it distinguishes original vs mutated.
3. **REFACTOR** — Tighten the test; ensure CORRECT-7 (Conformance / Ordering /
   Range / Reference / Existence / Cardinality / Time).

The commit message links the mutant to the test, e.g.:

```
test(board): kill LIVED CONDITIONALS_NEGATION at board.go:194

Mutation: == → != in CanTransitionTo's forbidden-transition check.
Why LIVED: the rejected path returned an error of unspecified content;
  tests only checked `err != nil`.
Fix: assert on a substring of the error (e.g. "invalid transition from X to Y")
  so flipping the equality still produces a distinguishable message.
```

## Reporting

Each run writes a JSON report at `mutation-report.json` (gitignored) plus a
human summary on stderr. The baseline numbers go in
`MUTATION_TESTING_RESULTS.md` after each run.

## What is **not** in scope

- **Mutation annotations** (gremlins has none; go-mutesting has
  `// mutator-disable-next-line`). Per AGENTS.md §2.1, "missing UI = missing
  core". Suppressing mutants to make the score look better is exactly the
  pattern this technique is meant to *prevent*. If a real false-positive
  pattern shows up repeatedly (e.g. optimizer-only branches), it deserves a
  test of its own, not a suppression.
- **CI gate.** Gremlins supports thresholds, but adding a CI gate is its own
  decision. This ticket only adds the tooling.
- **Modifying production code to make mutants die.** The point is to find
  *test* weaknesses. A `LIVED` mutant that reveals a real semantic gap (e.g.
  `CanTransitionTo` returns an unhelpful error) should be killed by a better
  test, not by changing the production behavior.

## File layout

```
.gremlins.yaml                 # gremlins configuration
MUTATION_TESTING.md            # this design note
MUTATION_TESTING_RESULTS.md    # per-run baseline numbers + comments
mutation-baseline.json         # JSON report (gitignored)
scripts/run-mutation.sh        # wrapper that runs gremlins with the project's
                                # --tags and --coverpkg settings
```

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
gremlins unleash --diff=origin/main  # new mutants must die
```