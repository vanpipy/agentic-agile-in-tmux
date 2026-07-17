# Interaction

```mermaid
sequenceDiagram
  participant wiking
  participant plan
  participant feedback
  participant coding

  wiking ->> plan: wiki write plan
  coding ->> plan: coding read plan
  coding ->> feedback: coding write feedback
  wiking ->> feedback: wiking read feedback
  

## Rules

1. the plan wroten by wiki agent or wroten by others - now the wiki did not have a score
2. the coding agent read the plan and base on the exact code repository to evaluate the plan with a
   exact score
3. score >= 90 means the plan has high applicability for the real code repository, otherwise not
4. the coding agent will write the feedback to describe the score(>= 90) and coding agent accepts the plan
   as real guide to coding
5. the coding agent will write the feedback to describe the score(< 90) and the gaps between real and plan
6. so the plan&feedback loop has been built

## How to work

1. in daemon(awp) fire the wiki agent copy the plan into workspace from the wiki repository and rename the "{filename}-{index(default 0 and increased 1 each round)}" mark "--- end ---" below the last line in the wiki article
2. then, the daemon(awp) in ticking loop to check the work of wiki agent has done or not, once done
   invoke the coding agent to learn the target code repository and read the plan then let the coding agent write the feedback which is named "{filename}-feedback-{index}" include "--- end with {score} ---" below the last line in the feedback article
3. the daemon(awp) will get the "--- end with {score} ---" from "{filename}-feedback-{index}" to make a desision to continue or not.
   Continue 1->2 loop and end with syncing the plan(score >= 90) "{filename}-{index}" to original "{filename}"

## Discussion

### 1. The marker protocol is a hybrid: content-driven + in-band signal

The "--- end ---" and "--- end with {score} ---" lines are not just file content —
they are a tiny in-band protocol embedded in the artifacts.

| Audience | What they read | Why |
|---|---|---|
| **Daemon** | The score line | Cheap parse to decide continue/stop |
| **Next-round agent** | The full file body | Rich context to act on |

The marker is the daemon's view of the conversation; the body is the agent's
view. Two audiences share one artifact.

The alternatives (mtime heuristic, LLM judge, ticking agent) all have a
signal too — they just relocate it from the file tail to filesystem metadata
or LLM context. None escapes the need for a signal; the marker protocol is
the **cheapest, most deterministic** place to put it.

### 2. The loop moves the human out

The architecture is not:

```
wiking → [plan] → coding → [feedback] → HUMAN DECIDES → wiking → ...
```

It is:

```
wiking ↔ [files] ↔ coding
        ↑
   daemon (postman)
```

The agents are the conversation partners. The daemon is the postal service.
The human is gone from the inner loop — humans can rejoin at any time by
reading the file chain (`{filename}-0.md`, `{filename}-0-feedback.md`,
`{filename}-1.md`, ...) but are not in the critical path.

### 3. Two protocol elements, not one

- **Liveness**: `--- end ---` / `--- end with {score} ---` → "file is finalized"
- **Decision**: the score embedded in the marker → continue or sync

The agents do not call an "I'm done" API. They finish by *writing the right
last line*. The daemon just polls for the sentinel and parses the score.

This is crash-resilient (a half-written file has no sentinel), language-
agnostic (any agent that appends text can participate), and stateless on
the daemon side (no agent registry, no PID tracking).

### 4. Topology is the spec

The 2-cycle between wiking and coding is the **contract layer**:

```
    ┌──────────────┐  plan-{i}    ┌──────────────┐
    │   wiking     │ ───────────► │    coding    │
    │  (drafter)   │              │  (critic)    │
    │              │ ◄─────────── │              │
    └──────────────┘  feedback-{i}└──────────────┘
```

One back-edge. Everything else — filename suffix, marker line, postman tick,
score threshold — is bookkeeping to make that edge work.

| Layer | Question | Who decides |
|---|---|---|
| **Topology** | WHO talks to WHOM | the system architect |
| **Transport** | HOW messages move | the implementer |
| **Signal** | WHEN the loop ends | the implementer |
| **Daemon** | WHO orchestrates handoffs | the implementer |

Transport, signal, and daemon are *consequences* of the topology. Files,
markers, postman — all one way to implement a 2-cycle. Switch to in-memory
+ TERMINATE string + smart manager — still a 2-cycle, different packaging
of the same forced pattern.

### 5. The shape decides the way

Each topology forces its own implementation patterns:

| Shape | Forced termination | Forced daemon | Forced transport |
|---|---|---|---|
| Pipeline (A→B→C) | structural: "I am the last node" | sequential trigger | one-way |
| **2-cycle (A↔B)** | **content-based: signal in the message** | **shuttle / postman** | **bidirectional** |
| Star (hub⇄spokes) | hub-decides: "I say done" | hub (smart or delegated) | bilateral |
| Mesh (all⇄all) | emergent: consensus or timeout | speaker-selection | broadcast |
| Tree | structural: leaves done + parent aggregates | parent-child router | down/up |

The 2-cycle forces:
- A signal (no topology-level terminus → content must carry one)
- A shuttle daemon (must move bytes between A and B)
- Bidirectional transport (both edges must work)
- Bilateral liveness (each side needs to know "is the other done?")

File + marker + postman is not a free choice — it is the natural consequence
of a 2-cycle shape.

### 6. Comparison with AutoGen, LangGraph, CrewAI

| Framework | Native topology | Forced daemon | Forced termination | Forced transport |
|---|---|---|---|---|
| **AutoGen** | group chat (mesh-ish) + 1:1 | `GroupChatManager` (smart, LLM-driven) | `is_termination_msg` content check | in-memory messages |
| **LangGraph** | any graph (user-defined) | graph executor (dumb runtime) | reach END node (conditional edge) | in-memory state dict |
| **CrewAI** | pipeline + hierarchical tree | manager agent (smart, hierarchical) or none (sequential) | task count or manager-decides | in-memory task context |
| **This spec** | 2-cycle only | dumb postman | file-tail marker | files on disk |

Key observations:
- **AutoGen's `is_termination_msg` is the same idea as our marker** — content
  match for termination, just packaged differently.
- **LangGraph is a topology DSL** — could implement our 2-cycle trivially,
  gaining cycles, branches, parallelism for free.
- **CrewAI cannot natively express a 2-cycle** — sequential (pipeline) and
  hierarchical (tree) only, no cycles.
- **Our spec trades topology flexibility for operational simplicity** (dumb
  daemon, files, auditability, agent-agnostic).

### 7. External agents vs framework-owned agents

This is the root distinction. We use external agents (pi, codex, claude code,
aider, ...). AutoGen, CrewAI, LangGraph define their own.

This single decision **forces every other choice** in the design chain:

```
External agents (pi, codex, …)
  → agents don't share SDK
    → agents don't know about each other
      → communication via shared artifacts
        → files as transport (universal)
          → need liveness signal
            → file-tail marker
              → dumb postman daemon
                → awp as orchestrator
```

| | Framework-owned agents | External-agnostic orchestrator |
|---|---|---|
| **What the framework owns** | the agent itself (class, prompt, LLM, tools) | the topology only |
| **Cognitive load** | learn the framework's Agent API | learn the orchestration spec |
| **LLM choice** | configured per-agent in framework | chosen by your agent |
| **Capability ceiling** | what framework implements | whatever your agent can do |

In the wiking↔coding loop, **wiking and coding are not agents defined by us**.
They are **slots that any external agent can fill**:

```yaml
topology: 2-cycle
roles:
  wiking:
    agent: pi        # or codex, or claude code, or aider, …
    input: feedback-N-1.md, codebase-state
    output: plan-N.md
  coding:
    agent: codex     # different provider, doesn't matter
    input: plan-N.md, codebase
    output: feedback-N.md
```

The daemon does not care which agent you assign to which role. The
conversation happens **inside each agent's runtime**, against files. The
daemon just shuttles.

### 8. Connection to awp's existing philosophy

From `AGENTS.md` §3 Golden Rules:

> **Pi only** — no multi-agent abstraction, no `Agent` interface, no
> "adapter" for other agents.
>
> **Never modify pi source** — pi is a black box. Use extensions only.

awp already runs pi as an external agent. It does not define a unified
`Agent` interface. It calls pi via RPC, observes its output, does not care
how pi works internally.

**The wiking↔coding spec is awp's philosophy extended from 1-agent-
orchestrated to 2-agent-iterated.**

| awp today | This spec |
|---|---|
| 1 topology: "user drives N independent pi sessions" | 1 topology: "2-cycle between drafter and critic" |
| daemon: dumb TUI host | daemon: dumb postman |
| agents: pi (external) | agents: pi, codex, claude code, ... (any external) |
| transport: pi's RPC + UI render | transport: files |
| signal: pi's `exited` event | signal: file-tail marker |

Same philosophy, different shape. The competitive space is not "frameworks
vs us" — it is "**who wraps external agents best**", and the answer is
whoever picks the topology deliberately instead of fighting it.

### TL;DR

The wiking↔coding loop is a 2-cycle topology with file transport and
content-based termination, orchestrated by a dumb postman daemon. We wrap
external agents (pi, codex, ...) rather than defining our own, which is
what forces this design chain end-to-end. The marker protocol is the
cheapest possible signal; the topology is the spec; the shape decides the
way. awp's existing "Pi only, no Agent abstraction" rule is the foundation
of this design, not a limitation.
```
