# Mutation Testing Results

> **Tool:** `go-gremlins/gremlins v0.6.0`
> **Configs:** `.gremlins.yaml` (5 default mutators) and `.gremlins-all.yaml` (11 mutators)
> **Started:** 2026-07-02

## ⚠️ Important: gremlins full-module mode is buggy

`gremlins unleash` (no args, module-wide) is affected by
[issue #272](https://github.com/go-gremlins/gremlins/issues/272) and reports
many false-LIVED. **Per-package runs (`gremlins unleash <pkg>`) are
correct** and are the source of truth below.

| Mode | Killed | Lived | Not covered | Efficacy |
|---|---:|---:|---:|---:|
| Full module (`gremlins unleash`) | 86 | 702 | 1348 | **10.91%** (BROKEN) |
| Sum of per-package (5 mutators)  | 495 | **0** | 201 | **100%** |
| Sum of per-package (11 mutators) | **636** | **0** | 282 | **100%** |

The full-module result is a tool bug, not a real signal. All numbers below
are from per-package runs.

---

## Run 1 — Baseline (per-package, all 11 mutators)

Captured at the start of `task/awp-mutable-test` work, after the new
`session_model_change_first_wins_test.go` was added.

### Per-package

| Package | Killed | Lived | Not covered | Efficacy |
|---|---:|---:|---:|---:|
| cmd/awp | 11 | 0 | 55 | 100.00% |
| cmd/releasecheck | 6 | 0 | 4 | 100.00% |
| internal/board | 11 | 0 | 0 | 100.00% |
| internal/buildinfo | 16 | 0 | 0 | 100.00% |
| internal/config | 54 | 0 | 3 | 100.00% |
| internal/doctor | 29 | 0 | 2 | 100.00% |
| internal/git | 26 | 0 | 3 | 100.00% |
| internal/observability | 2 | 0 | 1 | 100.00% |
| internal/pi | 120 | 0 | 9 | 100.00% |
| internal/project | 83 | 0 | 15 | 100.00% |
| internal/release | 14 | 0 | 0 | 100.00% |
| internal/terminal | 243 | 0 | 188 | 100.00% |
| internal/update | 22 | 0 | 1 | 100.00% |
| **TOTAL** | **636** | **0** | **282** | **100.00%** |

### Per-mutator

| Mutator | Killed | Lived | Not covered |
|---|---:|---:|---:|
| ARITHMETIC_BASE | 137 | 0 | 50 |
| CONDITIONALS_BOUNDARY | 130 | 0 | 38 |
| CONDITIONALS_NEGATION | 281 | 0 | 41 |
| INCREMENT_DECREMENT | 11 | 0 | 7 |
| INVERT_NEGATIVES | 38 | 0 | 11 |
| INVERT_ASSIGNMENTS | 4 | 0 | 0 |
| INVERT_BITWISE | 1 | 0 | 0 |
| INVERT_BWASSIGN | 0 | 0 | 0 |
| INVERT_LOGICAL | 9 | 0 | 6 |
| INVERT_LOOPCTRL | 24 | 0 | 127 |
| REMOVE_SELF_ASSIGNMENTS | 1 | 0 | 2 |

### Interpretation

**Every covered mutant is killed.** The `awp` test suite, when run
correctly, achieves 100 % mutation efficacy. There are no real "test
quality" weaknesses on the covered code paths.

The 282 NOT COVERED mutants are **test gaps**, not test weaknesses:

- **INVERT_LOOPCTRL** (127 of 282) flips `continue` ↔ `break` inside
  `for` loops. Most of these are `continue` statements on no-op iterations
  (e.g. `if line[0] != '{' { continue }` after `scanner.Bytes()` returns an
  empty line). These branches are simply never reached in tests because the
  test data doesn't trigger them.
- **INVERT_NEGATIVES** (11) flips `-n` ↔ `+n` on numeric literals that
  are only ever used as lengths / limits, never as signed values.
- The remaining NOT COVERED are mostly on optional / fallback code paths
  (e.g. JSON entries with optional fields not present in test fixtures).

These represent **opportunities** to add tests, not bugs.

### Demonstration: closing two NOT COVERED mutations

`session_model_change_first_wins_test.go` (added this commit) closes two
gremlins NOT COVERED findings:

| Mutation | Before | After |
|---|---|---|
| `session.go:292:26` `== ""` → `!= ""` (model_change guard) | NOT COVERED | **KILLED** |
| `session.go:297:26` `== ""` → `!= ""` (thinking_level_change guard) | NOT COVERED | **KILLED** |

Manual RED verification (mutation applied by `sed`, test run):

```
$ sed -i '292s/info.ModelProvider == ""/info.ModelProvider != ""/' internal/pi/session.go
$ go test -count=1 -run TestParseSessionInfo_ModelChange_FirstWins ./internal/pi/
--- FAIL: TestParseSessionInfo_ModelChange_FirstWins (0.00s)
    session_model_change_first_wins_test.go:70: ModelProvider = "", want "anthropic" (first model_change wins)
    session_model_change_first_wins_test.go:74: ModelID = "", want "claude-opus-4" (first model_change wins)
FAIL
```

(Toggling the mutation back → test passes.)

The pattern: the original code's "first wins" guard wasn't tested because
no JSONL fixture had two `model_change` entries. The new test writes a
fixture with two such entries and asserts on the first winning.

---

## Future work

- **CI integration.** Add `gremlins --config .gremlins-all.yaml unleash
  --threshold-efficacy 0.85 --threshold-mutant-coverage 0.7` to a future CI
  pipeline. The current 100 % efficacy gives ample headroom.
- **Diff-mode in PRs.** Add a `gremlins unleash --diff=origin/main` step
  that runs only on changes vs main. Requires the gremlins bug to be fixed
  (or a wrapper script that runs per-package).
- **Closing more NOT COVERED mutations.** The 282 gaps are catalogued in
  the per-package JSON outputs. Closing them is incremental and independent.