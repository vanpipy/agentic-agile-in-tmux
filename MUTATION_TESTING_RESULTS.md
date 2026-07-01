# Mutation Testing Results

> **Tool:** `go-gremlins/gremlins v0.6.0`
> **Config:** `.gremlins.yaml` (mutators: arithmetic-base, conditionals-boundary, conditionals-negation, increment-decrement, invert-negatives)
> **Started:** 2026-07-02

## Baseline — Run 1 (full module)

Captured at the start of `task/awp-mutable-test` work.

```
Killed: 86
Lived:  702
Not covered: 1348
Timed out: 0
Not viable: 0
Test efficacy: 10.91%
Mutator coverage: 36.89%
Elapsed: 7m 56s
```

### Per-package

| Package | LIVED | KILLED | NOT_COV | Efficacy |
|---|---:|---:|---:|---:|
| internal/ui | 229 | 61 | 0 | 21.0% |
| internal/terminal | 183 | 0 | 0 | **0.0%** |
| internal/pi | 88 | 4 | 0 | 4.3% |
| internal/project | 58 | 7 | 0 | 10.8% |
| internal/config | 47 | 1 | 0 | 2.1% |
| internal/git | 25 | 0 | 0 | **0.0%** |
| internal/release | 14 | 0 | 0 | **0.0%** |
| internal/doctor | 14 | 7 | 0 | 33.3% |
| internal/buildinfo | 12 | 0 | 0 | **0.0%** |
| internal/update | 10 | 6 | 0 | 37.5% |
| internal/board | 8 | 0 | 0 | **0.0%** |
| cmd/awp | 7 | 0 | 0 | **0.0%** |
| cmd/releasecheck | 6 | 0 | 0 | **0.0%** |
| internal/observability | 1 | 0 | 0 | **0.0%** |
| **TOTAL** | **702** | **86** | **1348** | **10.9%** |

### Per-mutator

| Mutator | LIVED | KILLED | Efficacy |
|---|---:|---:|---:|
| **CONDITIONALS_NEGATION** | 501 | **0** | **0%** |
| **CONDITIONALS_BOUNDARY** | 108 | **0** | **0%** |
| ARITHMETIC_BASE | 48 | 86 | 64.2% |
| INVERT_NEGATIVES | 24 | 0 | 0% |
| INCREMENT_DECREMENT | 21 | 0 | 0% |

### Interpretation

**609 of 702 LIVED mutants (87%) are condition-related.**

The pattern is consistent: tests assert on `err != nil`, `result == true`, or
"non-empty output", but **never** on the exact relationship (`==`, `<`, `>`)
the code expresses. So flipping `==` to `!=` (CONDITIONALS_NEGATION) or `>` to
`>=` (CONDITIONALS_BOUNDARY) is undetected everywhere.

By contrast, ARITHMETIC_BASE has 64% efficacy — tests DO check exact numeric
results (e.g., `Slugify("hello", 40) == "hello"`), so flipping `+` to `-` in
`maxLen = 0` defaults flips the result and gets caught.

This is the canonical "coverage ≠ quality" lesson mutation testing is
designed to surface.

### Targeting plan

Starting with `internal/board` (smallest, 8 LIVED) to establish the
kill-mutant TDD loop, then `internal/config` (47 LIVED), then `internal/pi`,
`internal/project`, and `internal/git` in order of remaining low-hanging fruit.
`internal/ui` and `internal/terminal` are deferred — they have heavy rendering
surface and are dominated by `conditionals` mutations on render branches that
need architectural test changes (e.g., snapshot rendering tests).

---

## Run 2 — board package only

(Pending — applied changes recorded below as commits land.)