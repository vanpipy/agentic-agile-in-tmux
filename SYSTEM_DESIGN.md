# 🚀 agentic-with-pi (awp) — Technical Solution

> **A pi-native task collaboration board.**
> Run many pi sessions in parallel, in their own git worktrees, observe and steer them from a single TUI.

---

## 0. 文档定位

本文件是 `awp` 的**唯一设计规格**。所有架构、模块、接口、数据流的定义,均以此文件为准。

代码层、技术选型、具体实现策略可演进,**但本文件定义的产品形态、用户场景、关键流程不变**。

---

## 1. 产品定义

### 1.1 一句话定位

**awp = 多 pi session 并行协作看板**

用户场景:**同时跑 5 个 pi session**(改 bug、跑测试、读文档、写 demo、调 prompt),在一个 TUI 里**看到它们各自在做什么、给它们发指令、在它们之间切换**。

### 1.2 与同类工具的边界

| 工具 | 定位 | 与 awp 的关系 |
|------|------|--------------|
| `pi` CLI | 单 session 交互式 AI coding | **awp 的核心引擎** |
| `tmux` | 通用终端复用 | awp **不用**(早期模板已证明 PTY 路线更好) |
| 早期 fork 模板(多 agent kanban) | 多 agent 通用 kanban(支持 6 种 agent) | awp 的**历史架构模板**(现为独立项目,配置独立) |
| `claude code` / `opencode` 等 | 各自封闭的交互式 AI | **不兼容**——awp 只服务 pi 用户 |

### 1.3 核心理念

> **"让 pi 的全部能力暴露在看板里"** —— 用户能看到的、可操作的、要做到和 pi 自己一致。

具体含义:
- pi 支持 30+ RPC 命令(`prompt`/`steer`/`abort`/`set_model`/`cycle_thinking`/...),awp 全部暴露对应 UI
- pi 的 session 文件就是事实之源,awp 不复制存储,只引用
- pi 的 extension 机制是"工具调用拦截"的官方接口,awp 通过写 extension 实现
- pi 的 `--mode rpc` 是结构化通信,awp **不用**自己造协议

---

## 2. Goals & Non-Goals

### 2.1 Goals(必须做到)

| # | 能力 | 用户故事 | 验收信号 |
|---|------|---------|----------|
| G1 | **多 pi 并行管理** | 同时跑 5 个 pi,一个看板统揽 | 卡片数 = 活跃 pi 数 |
| G2 | **嵌入式终端** | 不用跳出 TUI 看 pi 输出 | 嵌入 pane 与 pi TUI 一致 |
| G3 | **结构化事件流** | 看到 pi 每次工具调用的 args、results | 事件时间线有 type + 内容 |
| G4 | **双向 RPC** | TUI 发 prompt / steer / abort 给 pi | 操作 1s 内 pi 响应 |
| G5 | **pi session 生命周期** | 列出、恢复(`--continue`)、分叉(`--fork`)、命名 | `awp session ls/resume/fork` |
| G6 | **git worktree 隔离** | 每个 ticket 一个 worktree | `git worktree list` 看到所有 |
| G7 | **可选工具拦截** | 危险命令前弹模态框(用户决策) | `interception.enabled: true` 即可启用 |

### 2.2 Non-Goals(明确不做)

| # | 能力 | 不做的理由 |
|---|------|----------|
| N1 | **不兼容其他 agent** | 通用化 = 每种 agent 都浅;深度 = 只服务一类 |
| N2 | **不造跨进程协议** | pi 的 `--mode rpc` 已是 JSONL 协议,直接用 |
| N3 | **不做 OS-level 拦截**(SIGSTOP) | pi 的 extension + `ui.confirm` 是官方拦截点,更稳 |
| N4 | **不引入数据库** | ticket 存 JSON 文件(同原模板) |
| N5 | **不做多用户协作** | 单机、单用户、本地优先 |
| N6 | **不改 pi 源码** | pi 是黑盒,我们只消费 + 控制 |
| N7 | **不内建 pi 同步到云** | pi 自己有 dashboard / web,awp 不重复 |
| N8 | **不做主题编辑器** | 8 套预设主题够用,JSON 配置 |

---

## 3. 架构总览

### 3.1 系统拓扑

```
┌─────────────────────────────────────────────────────────────────────┐
│                         awp TUI (Bubble Tea)                         │
│  ┌─────────────────────────────────────────────────────────────┐    │
│  │ Header: project · filters · help · quit                    │    │
│  ├─────────────────────────────────────────────────────────────┤    │
│  │  Sidebar  │  Backlog  │  In Progress  │  Done             │    │
│  │  (项目)    │ (todo)    │  (running)    │  (归档)           │    │
│  │           │ ┌───────┐ │  ┌───────┐    │ ┌────┐            │    │
│  │ > my-app  │ │#42    │ │  │#43 ◐  │    │ │#40 │            │    │
│  │   sages   │ │auth   │ │  │bug    │    │ │... │            │    │
│  │           │ └───────┘ │  │pi:run │    │ └────┘            │    │
│  │           │           │  │bash   │    │                   │    │
│  │           │           │  └───────┘    │                   │    │
│  ├─────────────────────────────────────────────────────────────┤    │
│  │  Detail Pane (Event View | Terminal View)                  │    │
│  │  💬 14:23  You:  Find the auth bug                          │    │
│  │  🤖 14:24  pi:  Checking login flow...                      │    │
│  │  🛠️ 14:24  bash:  grep -rn "auth" src/                     │    │
│  │  🛠️ 14:24  read:  src/auth/login.ts                        │    │
│  │  🤖 14:25  pi:  Bug on line 42...                           │    │
│  │  ⏸  14:25  pi waiting for input  [Y]es [N]o [Steer] [Abort]│    │
│  ├─────────────────────────────────────────────────────────────┤    │
│  │ Statusbar: shortcuts · tick · errors                        │    │
│  └─────────────────────────────────────────────────────────────┘    │
└────────────────────────────────┬────────────────────────────────────┘
                                 │
              ┌──────────────────┴──────────────────┐
              │       PiClient (per ticket)          │
              │  ─────────────────────────────────  │
              │  stdin:  jsonl command writer        │
              │  stdout: jsonl event reader          │
              │  PTY:    embedded terminal pane      │
              │  ext:    ~/.awp/extension/awp.ts     │
              └──────────────────┬──────────────────┘
                                 │
                ┌────────────────┴────────────────┐
                ▼                                 ▼
        ┌──────────────────┐             ┌──────────────────┐
        │  pi process 1    │             │  pi process 2    │
        │  --mode rpc      │             │  --mode rpc      │
        │  --extension ... │             │  --extension ... │
        │  --session <id>  │             │  --continue      │
        └──────────────────┘             └──────────────────┘
                                 │
                                 ▼
                  ┌──────────────────────────────┐
                  │  ~/.pi/agent/sessions/        │
                  │  ├── --home-foo/             │
                  │  │   ├── 2026-06-17_abc.jsonl│
                  │  │   └── 2026-06-17_def.jsonl│
                  │  └── --home-bar/             │
                  │      └── ...                 │
                  └──────────────────────────────┘
```

### 3.2 关键设计原则

| 原则 | 体现 |
|------|------|
| **P1:PTY + RPC 双轨** | pi 启动时走 PTY(可见 loading、model 切换),RPC 准备好后切到 JSONL 解析。两者并存不冲突 |
| **P2:pi 是事实之源** | session 文件在 `~/.pi/agent/sessions/`,awp 只引用不复制 |
| **P3:扩展而非改造** | 拦截靠 pi extension,不靠 OS 信号;UI 增强靠 extension 不靠改 pi |
| **P4:可选的能力** | interception、terminal view 都是 opt-in。默认开箱即用,逐步开启高级功能 |
| **P5:本地优先** | 全部状态在本地 JSON + pi session 文件,无服务端 |
| **P6:深胜于广** | 只支持 pi,但 pi 的每一个 capability 都暴露对应 UI |

### 3.3 设计参照:早期 fork 自多 agent kanban 项目

awp 最初 fork 自**早期多 agent kanban 项目**(已 ship,118 commits, 4,974 行 UI),
采用 creack/pty + vt10x + Bubble Tea + bubbles 库。awp 已超出原模板范围,现在作为独立项目维护。

awp **复用以下模块**(从早期 fork 起步):

| 模块 | 复制范围 | 改动 |
|------|----------|------|
| `internal/terminal/`(`pty.go` + `vt.go` + `scrollback.go` + `selection.go`) | 整体(约 1,250 行) | 不改(纯 PTY/vt10x 绑定) |
| `internal/ui/model.go` | 整体(约 3,040 行) | 删多 agent 路由;加 pi 事件处理 hook;加拦截 / session picker Mode |
| `internal/ui/view.go` | 整体(约 1,930 行) | 改 `renderAgent*` → 固定 pi;加 `renderEventView` / `renderTerminalView` |
| `internal/board/`(`board.go` 160 行) | 整体 | 加 `PiSessionID` / `PiState` / `PiActivity` 字段(§5.1) |
| `internal/project/`(`project` + `store` + `tickets` + `filter`) | 整体 | 不改 |
| `internal/git/worktree.go`(249 行) | 整体 | 不改 |
| `internal/config/{theme,validate}.go` | 整体 | theme 不动;validate 简化(去掉 agent 字段校验) |
| `internal/testutil/`(143 行) | 整体 | 不改 |
| `internal/observability/` | 改 | 见 §3.4 — 扩展为 multi-handler(stderr + 文件)+ 加 crash 落盘 |
| `internal/update/` | 整体 | 不改 |
| `internal/app/`(`app.go` 159 行) | 整体 | 不改(启动 Bubble Tea program + 信号处理) |
| `cmd/{root,config,version}.go` | 整体 | 删多 agent 命令;加 `ticket` / `session` 子命令 |

**与原模板的关键结构差异**:
- 原模板把所有 UI 集中于 `ui/model.go` + `ui/view.go` 两文件(无 sub-package)
- awp 新增 `internal/ui/eventpane/`(结构化事件流渲染)和 `internal/ui/terminalpane/`(PTY 包装)作为 sub-package
- 理由:原模板单文件已达 5,000 行;awp 加 pi 事件 + 拦截会再加 ~1,000 行,集中在一文件会超 6,000 行,不利于维护
- eventpane / terminalpane 是独立的 `tea.Model` 实现,只暴露 `Update` + `View`,被主 Model 组合调用

**完全重新写**:
- `internal/pi/`(RPC 客户端、事件、session 扫描、extension) — 原模板无
- `internal/agent/`(PiPane = client + 事件订阅 + 状态机) — 原模板无此抽象
- `internal/ui/eventpane/`(结构化事件流,sub-Model) — 原模板无
- `internal/ui/terminalpane/`(PTY 嵌入 sub-Model) — 原模板在主 Model 直接持 `panes map`

### 3.4 Observability — 文件日志与 crash 捕获(2026-07-07)

**动机**:2026-07-07 用户运行时遇到 panic,awp 输出 `program was killed: program experienced a panic` 但 panic 值和 stack trace 跟着 Bubble Tea v1.3.10 的 `recoverFromPanic` 走 `fmt.Printf` 到 **stdout**,在 alt-screen 恢复时被滚动条冲掉。`ulimit -c = 0`,没有 core dump。事后无任何 artifact 可查。

**目标**:让下一次 panic 留有可分析的本地档案;不打扰正常使用。

**设计**:

| 项 | 值 | 理由 |
|---|---|---|
| 触发 | **始终开启**(stderr + 文件) | 静默失败正是要修的痛点;不能再依赖用户记得加 `--debug` |
| 默认 level | `Warn` | 正常使用时每天 <10KB,无干扰;一旦出错必落盘 |
| `--debug` 行为 | 提升到 `Debug` | 保持现有语义;**只是现在 Debug 也写文件** |
| Log 目录 | `~/.awp/logs/` | 见 §5.4 config example `log.dir`(design 早已预留) |
| 覆盖 | 环境变量 `AWP_LOG_DIR`(测试) | 单元测试不污染 home |
| 文件命名 | `awp-YYYY-MM-DD.log` | 一天一文件,按日期轮转 |
| 格式 | stderr = text(含 source);file = JSON | 终端易读;文件可 `jq` 解析 |
| Retention | 7 天,启动时清理 | 一次性的 `os.ReadDir` + 按 mtime 删旧 |
| Multi-handler | 自实现(`slog.Handler` 链) | 不用第三方依赖,见 `internal/observability/logger.go` `multiHandler` |
| Fallback | 目录创建失败 → 只 stderr + 启动时打印一条 warning | 不让 observability 阻断 awp 启动 |
| Crash 捕获 | `runTUI()` 加 `defer recover()` 包装 `prog.Run()` | 比升级 bubbletea v2 改动小 100x |
| Crash 文件 | `awp-crash-YYYY-MM-DD-HHMMSS-<pid>.log` | 与日 log 分离;含 panic 值 + `runtime.Stack(all=true)` + 日 log 最后 100 行 |

**关键决策**:
1. **不升级 bubbletea v2**。v2 的 panic 落盘是“官方”解,但 API 是 breaking,涉及 model / view / 全部 key handler。AGENTS.md §2.1 抗捷径原则。
2. **不添加 `--log-file` flag**。和动机矛盾 — 下一个 crash 时用户不会再记得加 flag。
3. **crash 时 re-panic**。我们的 `defer recover()` 先写文件,然后 `panic(r)` 重新抛给 Bubble Tea 的外层 recover,它仍能恢复 alt-screen。文件不依赖终端状态。

**API 变更**:
```go
// Before:
observability.Init(debug bool)
// After:
observability.Init(debug bool, logDir string) error

// New:
observability.WriteCrashFile(logDir string, r any, stack []byte) (string, error)
```

**测试要求**(TDD §2.2, §4.2):
- 默认 level 为 `Warn`(Debug/Info 在 default 下不出文件)
- `--debug` 提升到 `Debug`
- `Debug/Info/Warn/Error` 全部落文件
- `logDir` 不可写时回退到 stderr 且不 panic
- 启动时清理 7 天前文件
- `WriteCrashFile` 包含 panic 值和 stack,文件名前缀 `awp-crash-`

---

## 4. 目录结构

```
awp/
├── main.go                              # 调 cmd.Execute
├── cmd/awp/
│   ├── root.go                          # Cobra root + 默认子命令
│   ├── ticket.go                        # ticket new/ls/rm
│   ├── session.go                       # session ls/resume/fork
│   ├── project.go                       # project new/list/delete
│   ├── config.go                        # config generate/validate/path
│   ├── doctor.go                        # 自检
│   └── version.go
├── internal/
│   ├── app/app.go                       # 启动编排
│   ├── ui/
│   │   ├── model.go                     # 主 Model(13 种 Mode),从早期 fork 起步
│   │   ├── view.go                      # 渲染,整复制改
│   │   ├── eventpane/                   # ★ 新:结构化事件流(sub-Model)
│   │   │   ├── model.go                 #   ~300 行
│   │   │   └── model_test.go
│   │   └── terminalpane/                # ★ 新:PTY pane 嵌入包装(sub-Model)
│   │       └── model.go                 #   ~150 行
│   ├── terminal/                        # 从早期 fork 起步(约 1,250 行)
│   │   ├── pty.go                       #   creack/pty 包装
│   │   ├── vt.go                        #   vt10x 包装
│   │   ├── scrollback.go                #   环形缓冲
│   │   ├── selection.go                 #   鼠标文本选择
│   │   ├── pty_test.go
│   │   ├── scrollback_test.go
│   │   └── selection_test.go
│   ├── pi/                              # ★ pi-mono 深度集成
│   │   ├── client.go                   # RPC 客户端(stdin/stdout JSONL)
│   │   ├── session.go                  # 扫描 ~/.pi/agent/sessions/
│   │   ├── events.go                   # pi AgentEvent → 内部 PiEvent
│   │   ├── prompt.go                   # ticket 上下文注入
│   │   ├── commands.go                 # 30+ RPC 命令的 Go 包装
│   │   └── extension/                  # ★ pi extension 源码
│   │       ├── awp-extension.ts        # 拦截 + 自定义 UI 桥接
│   │       └── README.md
│   ├── agent/                           # PiPane 抽象
│   │   ├── pane.go                     # PiPane = PiClient + 事件订阅 + 状态
│   │   ├── status.go                   # PiState 状态机
│   │   └── eventbuffer.go              # 事件环形缓冲
│   ├── board/
│   │   ├── board.go                    # Ticket / Column 类型
│   │   └── board_test.go
│   ├── project/
│   │   ├── project.go
│   │   ├── store.go                    # ~/.config/awp/projects.json
│   │   ├── tickets.go                  # ~/.config/awp/tickets/{id}.json
│   │   └── filter.go
│   ├── git/worktree.go                  # git worktree 包装
│   ├── config/
│   │   ├── config.go                    # JSON config
│   │   ├── theme.go                     # 8 套主题
│   │   └── validate.go
│   ├── observability/                   # ★ 多 handler 日志(stderr + 文件)+ crash 捕获
│   │   ├── logger.go                    #   Init(debug, logDir) + multi-handler + 7 天 retention
│   │   ├── crash.go                     #   WriteCrashFile — panic 值 + runtime.Stack 落盘
│   │   └── *_test.go
│   ├── testutil/                        # TestEnv
│   └── update/                          # GitHub release 检查
├── docs/
│   ├── PI_INTEGRATION.md                # pi 集成详细说明
│   ├── DATA_MODEL.md
│   ├── UI_DESIGN.md
│   └── CONFIGURATION.md
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── AGENTS.md                            # 给 pi agent 看的工作指南
├── SYSTEM_DESIGN.md                     # ← 本文件
└── .sages/workspace/                    # MDD workflow 工作区
```

**总代码量估算**:~11,000-12,000 行(从早期 fork 的 10,498 增长 ~10%,主要是新增的 pi 集成模块)

---

## 5. 数据模型

### 5.1 Ticket — 任务单元

```go
// internal/board/board.go
type Ticket struct {
    ID          TicketID     `json:"id"`
    ProjectID   string       `json:"project_id"`
    Title       string       `json:"title"`
    Description string       `json:"description,omitempty"`
    Status      TicketStatus `json:"status"`     // backlog / in_progress / done / archived
    Priority    int          `json:"priority"`   // 1-5
    Labels      []string     `json:"labels,omitempty"`

    // Git 集成
    UseWorktree  bool   `json:"use_worktree"`
    WorktreePath string `json:"worktree_path,omitempty"`
    BranchName   string `json:"branch_name,omitempty"`
    BaseBranch   string `json:"base_branch,omitempty"`

    // ★ Pi session 绑定
    PiSessionID   string    `json:"pi_session_id,omitempty"`     // pi UUID v7
    PiSessionPath string    `json:"pi_session_path,omitempty"`  // ~/.pi/agent/sessions/.../x.jsonl
    PiSpawnedAt   time.Time `json:"pi_spawned_at,omitempty"`
    PiState       PiState   `json:"pi_state"`                    // idle/streaming/thinking/...
    PiActivity    string    `json:"pi_activity,omitempty"`       // "running bash", "reading file.go"
    PiModel       string    `json:"pi_model,omitempty"`          // "anthropic/claude-sonnet-4"
    PiThinking    string    `json:"pi_thinking,omitempty"`       // off/minimal/low/medium/high/xhigh

    // 元数据
    CreatedAt   time.Time  `json:"created_at"`
    UpdatedAt   time.Time  `json:"updated_at"`
    StartedAt   *time.Time `json:"started_at,omitempty"`
    CompletedAt *time.Time `json:"completed_at,omitempty"`
}
```

### 5.2 PiState — pi 内部状态

```go
type PiState string

const (
    PiStateNone         PiState = "none"          // ticket 无 pi
    PiStateStarting     PiState = "starting"      // 启动中
    PiStateIdle         PiState = "idle"          // 等 prompt
    PiStateStreaming    PiState = "streaming"     // 生成 token
    PiStateThinking     PiState = "thinking"      // thinking_level 推理
    PiStateToolCall     PiState = "tool_call"     // 工具调用中
    PiStateAwaitingUser PiState = "awaiting_user" // 等用户输入(confirm/input/editor)
    PiStateCompacting   PiState = "compacting"    // 上下文压缩中
    PiStateRetrying     PiState = "retrying"      // 自动重试中
    PiStateError        PiState = "error"
    PiStateExited       PiState = "exited"        // 进程退出
)
```

**状态来源优先级**:
1. **RPC 事件**(最权威)— `agent_start` / `turn_start` / `tool_execution_start` / `message_start` / `compaction_start` / `auto_retry_start`
2. **PTY 屏幕 fallback**(RPC 未就绪时)— 通过 vt10x 解码 + 正则匹配
3. **进程信号** — `ExitMsg` 来自 PTY 读 EOF

### 5.3 PiSessionInfo — 扫描 `~/.pi/agent/sessions/`

```go
// internal/pi/session.go
type PiSessionInfo struct {
    ID            string    // pi UUID v7
    Path          string    // 完整 .jsonl 路径
    CWD           string
    Timestamp     time.Time
    ParentID      string    // fork 来源
    Branch        string    // 通过 session name 推断
    ModelProvider string
    ModelID       string
    ThinkingLevel string
    MessageCount  int
    FirstPrompt   string    // 截取前 80 字符
    LastAssistant string    // 截取最后一条 assistant text
}
```

**扫描策略**:不调用 pi 进程,直接读 `.jsonl` 第一行(头)+ 后续 entry 摘要。延迟低,不消耗 pi 资源。

### 5.4 配置 schema(`~/.config/awp/config.json`)

```json5
{
  "defaults": {
    "pi_binary": "pi",
    "worktree_base": "",                    // 默认 {repo}-worktrees
    "branch_prefix": "awp/",
    "branch_template": "{prefix}{slug}",
    "slug_max_length": 40,
    "init_prompt_template": "你正在 awp 看板的 ticket「{title}」上工作。\n\n{body}\n\n分支: {branch},工作目录: {worktree}"  // 可改
  },
  "ui": {
    "theme": "catppuccin-mocha",            // 8 套预设之一
    "default_detail_view": "events",        // "events" | "terminal"
    "scrollback_lines": 10000,
    "show_sidebar": true,
    "sidebar_width": 24,
    "show_status_bar": true,
    "mouse_enabled": true
  },
  "pi": {
    "thinking_level": "medium",
    "model": {                              // 可选,null = 用 pi 默认
      "provider": null,
      "model_id": null
    },
    "extensions": [                         // 启动时 --extension 加载
      "~/.awp/extension/awp-extension.ts"
    ],
    "trust_project": "ask",                 // "always" | "ask" | "never"
    "max_concurrent_pis": 20,               // 同时活跃的 pi 数量上限
    "session_resume_preference": "continue" // "continue" | "resume-picker" | "always-new"
  },
  "interception": {                         // ★ 可选:工具调用拦截
    "enabled": false,                        // 默认关
    "block_tools": ["bash"],                 // 哪些工具调用前要确认
    "block_patterns": [                      // bash 命令 glob 黑名单
      "rm -rf /*",
      "sudo *",
      "curl * | sh",
      "git push --force *"
    ],
    "allow_patterns": [                      // 白名单(优先于黑名单)
      "rm -rf /tmp/*",
      "rm -rf ~/.cache/*"
    ],
    "auto_approve_after_seconds": 30,        // 30s 不响应自动批准
    "show_command_diff": true                // 弹窗显示命令 + 上下文
  },
  "cleanup": {
    "delete_worktree_on_done": true,
    "delete_branch_on_done": false,
    "force_worktree_removal": false
  },
  "behavior": {
    "confirm_quit_with_running_pi": true,
    "auto_refresh_agents_ms": 1000
  },
  "log": {
    "level": "info",                         // "debug" | "info" | "warn" | "error"
    "dir": "~/.awp/logs"
  }
}
```

### 5.4 Ticket Status 状态机(2026-06-28 简化)

Ticket 状态转换由 `board.Ticket.CanTransitionTo(target TicketStatus) error` 强制验证。

**2026-06-28 重大简化**:状态机从 4 状态(`backlog` / `in_progress` / `done` / `archived`)缩减为 **2 状态**(`backlog` / `in_progress`)。原因为:

- "完成"是用户的判断,不是状态机能编码的事——用户决定做完了,按 `d` 键**删除**任务,而不是把它推到 `done` / `archived`
- 4 状态机的语义设计过度(尤其是 `archived` 终态带来的"看不到/找不到/恢复不了"等 UX 问题)
- 完成历史记录由 git log + pi session log 承担,不需要看板再记一份

**新状态机**——无终态、无归档、无完成:

```
backlog ⇄ in_progress
```

| 转换 | 允许？ | 说明 |
|---|---|---|
| `backlog` → `in_progress` | ✅ | Space 键:开始工作 |
| `in_progress` → `backlog` | ✅ | Space 键:暂停 / 重新考虑优先级 |
| `backlog` → `backlog` | ✅ | no-op |
| `in_progress` → `in_progress` | ✅ | no-op |
| 其它 | — | 不存在 |

**Orphan-agent 守卫**:`in_progress` → `backlog` 在 `AgentStatus == AgentWorking` 时**被 UI 层拒绝**(避免孤立运行中的 pi 子进程)。状态机本身保持纯函数(agent 语义不属于 board package)。

**删除即完成**:用户按 `d` 键时,任务被删除(worktree + branch + ticket 记录一起清)。这是不可逆操作;`git log` + pi session log 是恢复已完成工作的唯一来源。

**数据兼容**:加载旧 JSONL 时,`status == "done"` 或 `status == "archived"` 的 ticket 会被静默丢弃(用户接受的破坏性变更)。

UI 拒绝时通过通知 toast 反馈:`Move rejected: cannot move ticket to backlog while agent is working (stop the agent first)`。

---

## 6. Pi 集成 — 设计的核心

### 6.1 启动一个 pi 实例

```go
// internal/pi/client.go (骨架)
type PiClient struct {
    cmd       *exec.Cmd
    pty       *os.File              // PTY fd
    rpcReader *json.Decoder         // 解析 stdout JSONL
    rpcWriter *json.Encoder         // 写 stdin JSONL
    sessionID string

    eventSink  chan<- PiEvent       // 异步投递到 TUI
    rawOutput  chan<- []byte        // 启动阶段原始字节(给 PTY pane)
    mu         sync.Mutex
    closed     atomic.Bool
}

type StartOptions struct {
    CWD            string
    SessionID      string            // 已有 → --session
    ContinueLast   bool              // 无 SessionID 且有历史 → --continue
    Prompt         string            // 初始 prompt
    Name           string            // session 名称(显示用)
    ThinkingLevel  string
    Extensions     []string          // --extension path(可多个)
    Env            map[string]string // 额外环境变量
}

func (c *PiClient) Start(ctx context.Context, opts StartOptions) error {
    args := []string{"--mode", "rpc"}

    // session 恢复优先级:显式 ID > continue > 新建
    if opts.SessionID != "" {
        args = append(args, "--session", opts.SessionID)
    } else if opts.ContinueLast {
        args = append(args, "--continue")
    }
    if opts.Name != "" {
        args = append(args, "--name", opts.Name)
    }
    if opts.ThinkingLevel != "" {
        args = append(args, "--thinking", opts.ThinkingLevel)
    }
    for _, ext := range opts.Extensions {
        args = append(args, "--extension", ext)
    }
    if opts.Prompt != "" {
        args = append(args, opts.Prompt)  // 初始 prompt 作为命令行参数
    }

    c.cmd = exec.CommandContext(ctx, opts.Binary, args...)
    c.cmd.Env = buildPiCleanEnv(opts.Env)

    // ★ 关键决策:用 PTY 启动,即使走 RPC 模式
    // 原因:启动阶段(loading、model init)是控制台输出,不是 JSONL
    //      RPC 模式启动后切到 JSONL,识别切换点继续解析
    ptmx, err := pty.Start(c.cmd)
    c.pty = ptmx

    c.rpcReader = json.NewDecoder(ptmx)
    c.rpcWriter = json.NewEncoder(ptmx)

    go c.readLoop()
    return nil
}

func (c *PiClient) readLoop() {
    // bufio.Scanner 处理可能跨多行的大 JSON
    scanner := bufio.NewScanner(c.pty)
    scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)  // 4MB max line

    isRPCReady := false  // 启动序列检测

    for scanner.Scan() {
        line := scanner.Bytes()
        if c.closed.Load() { return }

        if !isRPCReady {
            // 启动阶段:全部作为 raw output 发给 PTY pane
            c.rawOutput <- append([]byte{}, line...)
            // 检测 JSONL 切换:第一行 `{` 开头
            if len(line) > 0 && line[0] == '{' {
                isRPCReady = true
                // 这一行也解析为 JSON
            } else {
                continue
            }
        }

        // RPC 阶段
        var ev struct {
            Type    string          `json:"type"`
            ID      string          `json:"id"`
            Command string          `json:"command"`
            Success bool            `json:"success"`
            Data    json.RawMessage `json:"data"`
            Error   string          `json:"error"`
            // 事件字段(agent_start / tool_execution_* / etc.)
            ToolName      string          `json:"tool_name,omitempty"`
            Args          json.RawMessage `json:"args,omitempty"`
            Result        json.RawMessage `json:"result,omitempty"`
            IsError       bool            `json:"is_error,omitempty"`
            PartialResult json.RawMessage `json:"partial_result,omitempty"`
            Message       json.RawMessage `json:"message,omitempty"`
            Content       string          `json:"content,omitempty"`
            AssistantEv   json.RawMessage `json:"assistantMessageEvent,omitempty"`
            WillRetry     bool            `json:"willRetry,omitempty"`
        }
        if err := json.Unmarshal(line, &ev); err != nil {
            c.eventSink <- PiEvent{Type: "parse_error", Error: err.Error(), Raw: line}
            continue
        }
        c.eventSink <- PiEvent{
            Type:      ev.Type,
            ID:        ev.ID,
            Command:   ev.Command,
            Success:   ev.Success,
            Data:      ev.Data,
            Error:     ev.Error,
            // ... 复制其他字段
        }
    }

    if err := scanner.Err(); err != nil {
        c.eventSink <- PiEvent{Type: "process_exit", Error: err.Error()}
    } else {
        c.eventSink <- PiEvent{Type: "process_exit", Code: 0}
    }
}
```

### 6.2 Pi 事件 → 内部 PiEvent

```go
// internal/pi/events.go
type PiEvent struct {
    // 通用字段
    Type    string          // agent_start | agent_end | turn_start | turn_end |
                            // message_start | message_update | message_end |
                            // tool_execution_start | tool_execution_update | tool_execution_end |
                            // queue_update | compaction_start | compaction_end |
                            // auto_retry_start | auto_retry_end |
                            // session_info_changed | thinking_level_changed |
                            // extension_ui_request | extension_error |
                            // process_exit | parse_error
    ID      string          // 命令 ID(响应相关)
    Success bool
    Error   string
    Data    json.RawMessage // 响应 data 字段

    // 工具调用字段
    ToolName      string          // "bash" / "read" / "edit" / "write" / "find" / "grep" / "ls"
    ToolArgs      json.RawMessage // 工具参数(原始 JSON)
    ToolResult    json.RawMessage
    IsError       bool
    PartialResult json.RawMessage // 增量输出(用于 bash streaming)

    // 消息字段
    Message     json.RawMessage // AgentMessage 完整对象
    Content     string          // 文本内容(assistant text 增量)
    StreamEvent json.RawMessage // AssistantMessageEvent 增量

    // queue
    Steering  []string
    FollowUp  []string

    // 进程级
    ExitCode int
    WillRetry bool
}
```

### 6.3 内部 PiState 推导

```go
// internal/agent/status.go
func updatePiState(current PiState, ev PiEvent) PiState {
    switch ev.Type {
    case "agent_start":
        return PiStateStreaming
    case "turn_start":
        return PiStateThinking
    case "message_start":
        return PiStateStreaming
    case "message_update":
        if containsThinkingToken(ev.StreamEvent) {
            return PiStateThinking
        }
        return PiStateStreaming
    case "tool_execution_start":
        return PiStateToolCall
    case "compaction_start":
        return PiStateCompacting
    case "auto_retry_start":
        return PiStateRetrying
    case "agent_end":
        return PiStateIdle
    case "process_exit":
        return PiStateExited
    case "extension_ui_request":
        return PiStateAwaitingUser
    }
    return current
}

func activityFromToolCall(toolName string, args json.RawMessage) string {
    switch toolName {
    case "bash":
        var a struct{ Command string `json:"command"` }
        json.Unmarshal(args, &a)
        return "running: " + truncate(a.Command, 40)
    case "read":
        var a struct{ Path string `json:"path"` }
        json.Unmarshal(args, &a)
        return "reading: " + a.Path
    case "edit":
        var a struct{ Path string `json:"path"` }
        json.Unmarshal(args, &a)
        return "editing: " + a.Path
    case "write":
        var a struct{ Path string `json:"path"` }
        json.Unmarshal(args, &a)
        return "writing: " + a.Path
    case "find", "grep":
        return "searching"
    case "ls":
        return "listing"
    }
    return "tool: " + toolName
}
```

### 6.4 双向 RPC 命令

```go
// internal/pi/commands.go
type RpcCommand struct {
    ID      string          `json:"id,omitempty"`
    Type    string          `json:"type"`
    Payload json.RawMessage `json:"-"`  // 用具体类型
}

// Prompting
func (c *PiClient) SendPrompt(msg string) error  { return c.send("prompt", &rpcCmdPrompt{Message: msg}) }
func (c *PiClient) Steer(msg string) error       { return c.send("steer", &rpcCmdSteer{Message: msg}) }
func (c *PiClient) FollowUp(msg string) error    { return c.send("follow_up", &rpcCmdFollowUp{Message: msg}) }
func (c *PiClient) Abort() error                { return c.send("abort", nil) }
func (c *PiClient) NewSession(parent string) error { return c.send("new_session", &rpcCmdNewSession{ParentSession: parent}) }

// State
func (c *PiClient) GetState() (*RpcSessionState, error) { ... }

// Model
func (c *PiClient) SetModel(provider, modelID string) error { ... }
func (c *PiClient) CycleModel() error { ... }
func (c *PiClient) GetAvailableModels() ([]ModelInfo, error) { ... }

// Thinking
func (c *PiClient) SetThinkingLevel(level string) error { ... }
func (c *PiClient) CycleThinkingLevel() error { ... }

// Compaction
func (c *PiClient) Compact(instructions string) error { ... }
func (c *PiClient) SetAutoCompaction(enabled bool) error { ... }

// Session
func (c *PiClient) SetSessionName(name string) error { ... }
func (c *PiClient) SwitchSession(path string) error { ... }
func (c *PiClient) Fork(entryID string) error { ... }
func (c *PiClient) Clone() error { ... }
func (c *PiClient) GetSessionStats() (*SessionStats, error) { ... }
func (c *PiClient) ExportHTML(path string) error { ... }

// Bash(直接执行,不通过 LLM)
func (c *PiClient) Bash(command string) (*BashResult, error) { ... }
func (c *PiClient) AbortBash() error { ... }

// Extension UI 响应
func (c *PiClient) RespondExtensionUI(reqID, value string) error { ... }
func (c *PiClient) ConfirmExtensionUI(reqID string, confirmed bool) error { ... }
func (c *PiClient) CancelExtensionUI(reqID string) error { ... }
```

每个命令都生成 ID,响应通过 `cmd_id` 关联。

### 6.5 Pi Extension — 拦截机制(可选)

```typescript
// internal/pi/extension/awp-extension.ts
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

export default function (pi: ExtensionAPI) {
    // 1) 工具调用拦截(LLM 触发)
    pi.on("tool_call", async (event, ctx) => {
        if (shouldIntercept(event)) {
            const decision = await askAwp({
                kind: "tool_call",
                tool: event.toolName,
                args: event.input,
            });
            if (decision === "deny") {
                return { block: true, reason: "Denied by awp" };
            }
        }
        return;
    });

    // 2) 用户 bash 拦截(用户在 pi TUI 里直接敲命令)
    pi.on("user_bash", async (event, ctx) => {
        if (shouldInterceptBash(event.input.command)) {
            const decision = await askAwp({ kind: "user_bash", command: event.input.command });
            if (decision === "deny") {
                return { block: true };
            }
        }
        return;
    });

    // 3) 桥接 pi 的 input/confirm/select UI 到 awp 模态框
    pi.on("input", async (event, ctx) => {
        return await askAwp({ kind: "input", prompt: event.prompt });
    });
    // 同样的 for "confirm", "select", "editor"
}

async function askAwp(question: object): Promise<any> {
    // 通过 Unix socket 问 awp
    // socket path: ~/.awp/socket/extension-{ticket_id}.sock
    // 协议:JSON request, JSON response
    return new Promise((resolve, reject) => {
        const reqId = crypto.randomUUID();
        const req = JSON.stringify({ id: reqId, ...question });
        // 写到 socket,等响应
        // ...
    });
}

function shouldIntercept(event: any): boolean {
    if (event.toolName === "bash") {
        const cmd = event.input?.command ?? "";
        // allow_patterns 优先
        if (matchesAny(cmd, getConfig("interception.allow_patterns"))) return false;
        // block_patterns
        if (matchesAny(cmd, getConfig("interception.block_patterns"))) return true;
    }
    return getConfig("interception.block_tools").includes(event.toolName);
}
```

**awp 端监听器**(可选,只在 `interception.enabled: true` 时启动):
```go
// internal/pi/extension/server.go
type InterceptionServer struct {
    listener net.Listener
    handlers map[string]chan InterceptionResponse  // reqID → response
}

func (s *InterceptionServer) Start(socketPath string) error { ... }
func (s *InterceptionServer) Handle(req InterceptionRequest) (InterceptionResponse, error) {
    // 投递到 TUI 弹模态框
    // 等用户响应(超时 = auto_approve_after_seconds)
    // 返回决定
}
```

### 6.6 Session 发现

```go
// internal/pi/session.go
type SessionStore struct {
    agentDir string  // ~/.pi/agent
}

func (s *SessionStore) List(cwdFilter string) ([]PiSessionInfo, error) {
    sessionsDir := filepath.Join(s.agentDir, "sessions")
    cwdKey := encodeCwdKey(cwdFilter)  // /home/foo → --home-foo--
    target := filepath.Join(sessionsDir, cwdKey)

    entries, _ := os.ReadDir(target)
    var sessions []PiSessionInfo
    for _, e := range entries {
        if !strings.HasSuffix(e.Name(), ".jsonl") { continue }
        path := filepath.Join(target, e.Name())
        info, err := parseSessionHeader(path)  // 只读首行
        if err != nil { continue }
        sessions = append(sessions, info)
    }
    sort.Slice(sessions, func(i, j int) bool {
        return sessions[i].Timestamp.After(sessions[j].Timestamp)
    })
    return sessions, nil
}

func (s *SessionStore) Read(path string) (*SessionContent, error) {
    // 读全部 entry,返回 messages 列表
    // 用于在 awp 里展示历史 session
}

func encodeCwdKey(cwd string) string {
    // pi 自己的逻辑:把 / 替换为 -
    abs, _ := filepath.Abs(cwd)
    return strings.ReplaceAll(strings.ReplaceAll(abs, "/", "-"), "\\", "-")
}
```

---

## 7. UI 设计

### 7.1 屏幕布局

```
┌─────────────────────────────────────────────────────────────────────┐
│  ◈ awp   project: my-app   filter: all   ? help  q quit            │ ← header
├──────────┬──────────┬──────────────────┬──────────┬────────────────┤
│ Projects │ Backlog  │ In Progress      │  Done    │  (no overflow) │
│ > my-app │ ┌──────┐ │  ┌──────┐        │ ┌──────┐ │                │
│   sages  │ │ #42  │ │  │ #43  │        │ │ #40  │ │                │
│ + add    │ │ auth │ │  │ bug  │        │ │ demo │ │                │
│          │ │ ◯    │ │  │ ◐    │        │ │ ✓    │ │                │
│          │ └──────┘ │  │bash  │        │ └──────┘ │                │
│          │ ┌──────┐ │  └──────┘        │          │                │
│          │ │ #41  │ │  ┌──────┐        │          │                │
│          │ │ docs │ │  │ #44  │        │          │                │
│          │ └──────┘ │  │ test │        │          │                │
│          │          │  └──────┘        │          │                │
├──────────┴──────────┴──────────────────┴──────────┴────────────────┤
│ Ticket #43  Fix bug in login                       [E]vents | [T]erm│ ← detail header
├─────────────────────────────────────────────────────────────────────┤
│  💬  14:23:01  You                                                     │
│      Find the auth bug and fix it                                    │
│  🤖  14:23:02  pi · claude-sonnet · medium thinking                  │
│      I'll check the login flow first.                                │
│  🛠️  14:23:45  bash                                                   │
│      $ grep -rn "auth" src/                                          │
│      Found 3 files...                                                │
│  🛠️  14:23:47  read  src/auth/login.ts                                │
│  🤖  14:24:12  pi                                                      │
│      The bug is on line 42: `if (user.expired) { ... }` should       │
│      check the token refresh timestamp.                               │
│  ⏸  14:24:12  pi waiting for input                                    │
│      [Y]es  [N]o  [S]teer  [A]bort  [→] next ticket                  │
├─────────────────────────────────────────────────────────────────────┤
│ [n] new [⏎] view [/] filter [s] spawn [S] stop [m] model [t] think  │ ← statusbar
└─────────────────────────────────────────────────────────────────────┘
```

**列宽自适应**:列数超出屏幕时,横向滚动(◀ ▶ 指示器)。

**Detail 切换**:
- `[E]vents`(默认)— 结构化事件流
- `[T]erminal` — PTY 嵌入,显示 pi 的真实 TUI(适合"我想看 pi 在想什么")

### 7.2 13 种 Mode(状态机)

```go
type Mode string
const (
    ModeNormal        Mode = "NORMAL"        // 看板浏览
    ModeCreateTicket  Mode = "CREATE"        // 新建 ticket
    ModeEditTicket    Mode = "EDIT"          // 编辑
    ModeEventView     Mode = "EVENTS"        // 事件流(默认详情)
    ModeTerminalView  Mode = "TERMINAL"      // 嵌入 PTY
    ModeConfirm       Mode = "CONFIRM"       // 确认弹窗
    ModeHelp          Mode = "HELP"          // 帮助
    ModeFilter        Mode = "FILTER"        // 搜索
    ModeSpawning      Mode = "SPAWNING"      // 启动 pi 中
    ModeShuttingDown  Mode = "SHUTDOWN"      // 关闭中
    ModeSettings      Mode = "SETTINGS"      // 设置
    ModeSessionPicker Mode = "PICK_SESSION"  // 选已有 session 恢复
    ModeInterception  Mode = "INTERCEPT"     // 工具调用拦截弹窗
)
```

#### 7.2.1 Mode 命名差异:awp vs 原模板

数字"13"是巧合(模板也是 13),**实际内容不同**。逐个对照:

| awp Mode | 原模板对应 | 差异说明 |
|----------|------------------|----------|
| `ModeNormal` | `ModeNormal` | 同 |
| `ModeCreateTicket` | `ModeCreateTicket` | 同 |
| `ModeEditTicket` | `ModeEditTicket` | 同 |
| `ModeEventView` | — | **新增**:结构化事件流,默认详情视图 |
| `ModeTerminalView` | `ModeAgentView` | **改名** + 加薄壳:聚焦 PTY 嵌入 |
| `ModeConfirm` | `ModeConfirm` | 同 |
| `ModeHelp` | `ModeHelp` | 同 |
| `ModeFilter` | `ModeFilter` | 同 |
| `ModeSpawning` | `ModeSpawning` | 同 |
| `ModeShuttingDown` | `ModeShuttingDown` | 同 |
| `ModeSettings` | `ModeSettings` | 同 |
| `ModeSessionPicker` | — | **新增**:选 pi session 恢复 |
| `ModeInterception` | — | **新增**:工具调用拦截弹窗(§6.5) |
| — | `ModeInsert` | **删除**:Vim-style insert mode,不需要 |
| — | `ModeCommand` | **删除**:Vim-style command mode,不需要 |
| — | `ModeCreateProject` | **删除**:不在看板内做,通过 CLI `awp project new` |

**Phase 7 实现**: 我们用 `ModeAgentView` 而非 `ModeTerminalView`(后者是 spec 名字)。
逻辑相同:全屏显示 PTY 终端,接管 PiPane.View()。原模板整段 `terminal.Pane` (1253行) 被 fork
到 `internal/terminal/pane.go`,并加 `HandleOutput(data []byte)` 让 PiClient 桥接 PTY 数据。

**统计**:8 个直接继承 + 3 个新增 + 1 个改名 + 3 个删除(净 +1)。

### 7.3 关键交互

| 键 | Mode | 行为 |
|---|------|------|
| `j` / `k` / `h` / `l` | Normal | 上下左右导航 |
| `n` | Normal | 新建 ticket → ModeCreateTicket |
| `e` | Normal | 编辑当前 ticket → ModeEditTicket |
| `space` / `-` | Normal | 右移 / 左移 ticket 到下一列 |
| `enter` | Normal | 切到 ModeEventView(或 ModeTerminalView,按上次) |
| `tab` | EventView / TerminalView | 切 E/T 视图 |
| `esc` / `ctrl+g` | EventView / TerminalView | 回到 ModeNormal |
| `d` | Normal | 删除 ticket(确认) |
| `s` | InProgress | 启动 pi → ModeSpawning |
| `S` | Running | 停止 pi(graceful 3s) |
| `m` | InProgress / EventView | 弹 model 选择 |
| `t` | 同上 | 弹 thinking level 选择 |
| `R` | 任意 | 刷新 |
| `/` | Normal | 搜索 → ModeFilter |
| `?` | 任意 | ModeHelp |
| `q` / `ctrl+c` | Normal | 退出(有 pi 在跑时 ModeConfirm) |
| `Y` / `N` | Interception | 批准/拒绝工具调用 |
| `→` | EventView | 下一条未读事件 |
| `c` | EventView | 复制当前事件内容 |

---

### 7.4 Per-Task Stop Notifications(每任务停止通知)

**需求来源**:`task/awp` ticket。10 个任务 A–J 并行运行时,用户在 B pane 工作,A
完成时需要 TUI 内部通知,不用主动切回 A 才能看到。

#### 7.4.1 触发事件

| 事件 | 含义 | 来源 | 实现 |
|------|------|------|------|
| **进程退出** | pi 子进程死亡(主动退出、崩溃、网络断开) | `terminal.ExitMsg` (已存在) | 现成 |
| **每轮完成** | assistant 发完最后一 token,`stopReason == "stop"`,等用户下一 prompt | pi session JSONL tail | 待实现 |

#### 7.4.2 通知策略

| 条件 | 行为 |
|------|------|
| 退出/完成的 pane 是当前 **focused** | **静默**(用户看得见 pane 状态,不需要 toast) |
| 退出/完成的 pane 是 **非 focused** | TUI 内部 toast 通知,文案带 ticket 标题 |
| ExitMsg 带非 nil `err` | 文案含"failed",触发 ✗ 图标(匹配 view.go:471-484 检测规则) |
| ExitMsg 带 nil `err` | 触发 ✓ 图标 |

#### 7.4.3 文案规范

| 场景 | Toast 字符串 | 图标 |
|------|--------------|------|
| focused pane 正常退出 | `Agent exited` | ✓ |
| focused pane 崩溃退出 | `Agent failed: <err>` | ✗ |
| 非 focused pane 正常退出 | `<ticket title> exited` | ✓ |
| 非 focused pane 崩溃退出 | `<ticket title> failed` | ✗ |
| 非 focused pane 每轮完成 | `<ticket title> finished a turn` | ✓ |

**图标选择约定**:view.go:471-484 通过 toast 字符串前缀 "Failed" / "Error" 或
子串 "failed" 决定 ✗ 渲染。`notifyExit` helper (model.go) 严格遵循此约定,
避免需要新增 view 层逻辑。

#### 7.4.4 实现要点

- **退出**走 `model.go` 的 `ExitMsg` handler。**每轮完成**通过 `pollAgentStatusesAsync`
  扩展,扫 pi session JSONL 找最后一条 assistant `message.stopReason`,边沿检测
  (`toolUse → stop` 转换时触发一次)。
- **不持久化**任何新增字段。Toast 是 ephemeral,3 秒后自动消失(`view.go:471`、
  `model.go:506-516` 的 `case notificationMsg`)。**实现**: `Init()` 调度
  `tickNotification(notificationTickInterval)`,handler 在每条 `notificationMsg`
  上检查 `time.Since(m.notifyTime) > notificationDuration` 并清空 toast;tick
  自维持 (handler 无条件 re-arm,与 `tickAgentStatus` 模式一致)。
  - 常量: `notificationDuration = 3s`, `notificationTickInterval = 500ms`
    (`internal/ui/model.go`)。见 commits `c24e035` + `b2398ac`。
  - 验收: `internal/ui/notify_auto_dismiss_test.go` + `notify_after_init_dies_test.go`。
  - 病史: c24e035 引入的 handler 在空状态返回 nil 导致 tick 提前死亡,b2398ac
    改为无条件 re-arm 才彻底修好。详见 `NOTIFY_DIAGNOSIS.md §6`。
- **不修改** `AgentCompleted` AgentStatus 字段,`view.go:400/1777` 的 ✓ 分支仍保持
  死代码(后续 ticket 单独处理持久化"已完成"状态)。
- **不引入** `saveTicket` 调用。已有 9 个调用点全跟用户事件绑定,新增不破坏此模式。
- **JSONL 解析**不能复用 `parseSessionInfo`(`maxScanLines = 200` 限制,真实 session
  可达 1226 行)。新增 `DetectLastStopReason(path)` 函数,反向扫描最后 4 KB 找到
  最后一条 assistant 消息。

#### 7.4.5 性能预算

| 指标 | 预算 |
|------|------|
| 空闲 pane 轮询成本 | < 2 µs / pane / poll(stat-only skip) |
| 活跃 pane 轮询成本 | < 2 ms / pane / poll(增量 JSONL 读取) |
| 10 个 pane 每 5 s 周期 | ~20 ms(0.4% 单核 CPU) |
| 30 个 pane 每 5 s 周期 | ~60 ms(1.1% 单核 CPU) |
| 每事件磁盘写入 | **0**(toast 是 ephemeral) |

#### 7.4.6 已知限制(明确不做)

- **多 toast 排队**:`m.notification` 是单字符串,10 个 pane 3 秒内都完成只看到最后
  一条。v1 接受,后续 ticket 处理。
- **焦点切换错失通知**:用户先在 A、A 完成(静默)、切到 B,A 的通知永远丢失。符合
  "focused pane 静默"规则,后续可加 focus-change 时回放。
- **退出 + 完成双通知**:一窗口内先完成一轮再退出,会发 2 次。不同前缀区分即可。

#### 7.4.7 参考

详细调研:见 `DONE_DETECTION_RESEARCH.md`(本仓库根目录)。

---

## 8. 关键流程

### 8.1 启动 ticket + 启动 pi

```
用户在 InProgress 列 ticket 上按 s
  │
  ▼
Model.spawnPi(ticket)
  │  1) ticket.PiSessionID 是否非空?
  │     - 是 → opts.SessionID = ticket.PiSessionID(恢复)
  │     - 否,ticket 之前 spawn 过(ticket.PiSpawnedAt != nil)?
  │       - 是 → opts.ContinueLast = true(--continue)
  │       - 否 → 新建
  │  2) worktree 不存在 → git worktree add
  │  3) 构造 init prompt 注入 ticket 上下文
  │
  ▼
Model.mode = ModeSpawning
Model.spawningTicketID = ticket.ID
  │
  ▼
return tea.Batch(m.spinner.Tick, m.doSpawn(ticket, opts))
  │
  ▼ (后台 goroutine)
PiClient.Start(ctx, opts)
  │  exec.CommandContext("pi", "--mode", "rpc", "--session", id, ...)
  │  pty.Start(cmd)  ← PTY 启动
  │  go c.readLoop()
  │
  ▼
PiClient 解析 stdout:
  - 启动序列(loading)→ raw output → PTY pane
  - 第一行 JSON → 切到 RPC 解析
  - agent_start / turn_start / tool_execution_start / ... → PiEvent
  │
  ▼
PiEvent → chan → tea.Msg(piEventMsg) → Model.Update()
  │
  ▼
Model 路由到 ticket:
  - ticket.PiState = updatePiState(...)
  - ticket.PiActivity = activityFromToolCall(...)
  - ticket.PiModel = ...(从 model_change 事件)
  - ticket.PiThinking = ...(从 thinking_level_change)
  - EventPane.Append(...)
  │
  ▼
ticket.PiSessionID = 从 session header 提取
m.panes[ticket.ID] = newPiPane
m.saveTicket(ticket)  ← 写盘
  │
  ▼
Model.mode = ModeEventView
m.focusedPane = ticket.ID
```

### 8.2 收到工具调用事件

```
PiClient.readLoop 收到 `{... "type":"tool_execution_start","tool_name":"bash","args":{"command":"grep ..."}}`
  │
  ▼
PiEvent{Type: "tool_execution_start", ToolName: "bash", ToolArgs: {...}}
  │
  ▼
Model.Update(piEventMsg) → handlePiEvent(ticketID, event)
  │
  ├─ ticket.PiState = PiStateToolCall
  ├─ ticket.PiActivity = "running: grep -rn auth src/"
  ├─ EventPane.Append(EvEntry{
  │     Type: EvToolCall,
  │     Tool: "bash",
  │     ArgsPretty: `grep -rn "auth" src/`,
  │     At: time.Now(),
  │   })
  │
  ▼
View() 重新渲染,卡片状态更新
  │
  ▼ (后续 100ms 内,stream 事件到达)
PiEvent{Type: "tool_execution_update", PartialResult: "Found 3 files..."}
  │
  ▼
EventPane.UpdateLastEntry(EvEntry{Result: "Found 3 files..."})
  │
  ▼ (工具执行完成)
PiEvent{Type: "tool_execution_end", Result: "...", IsError: false}
  │
  ▼
ticket.PiState = PiStateStreaming(下一个 turn 开始)
EventPane.FinalizeLastEntry(EvEntry{Result: "...", Duration: 1.2s})
```

### 8.3 Interception 流程(可选,只在 enabled 时)

```
pi 即将执行 `rm -rf /tmp/old`,LLM 触发 tool_call
  │
  ▼
awp-extension.ts 收到 tool_call event
  │  - toolName == "bash",command 匹配 block_patterns
  │  - 通过 socket 问 awp
  │
  ▼
awp.InterceptionServer 收到请求
  │  投递 InterceptionRequestMsg 到 TUI
  │
  ▼
Model.Update → ModeInterception
  │
  ▼
View() 渲染模态框(全屏覆盖):
  ┌─ Awaiting Decision ──────────────────────┐
  │                                          │
  │  ⚠ pi wants to execute:                  │
  │                                          │
  │    $ rm -rf /tmp/old                     │
  │                                          │
  │  in worktree:                            │
  │    ~/worktrees/awp-auth-fix/             │
  │                                          │
  │  matched:                                │
  │    block_pattern: "rm -rf /*"            │
  │                                          │
  │  ⏱  auto-approve in 30s                  │
  │                                          │
  │  [Y] Approve    [N] Deny    [A] Always  │
  │                                          │
  └──────────────────────────────────────────┘
  │
  ▼
用户按 N
  │
  ▼
Model 调 InterceptionServer.Respond(reqID, "deny")
  │
  ▼
extension 收到响应 → 返回 { block: true, reason: "Denied by awp" }
  │
  ▼
pi 收到 block,跳过工具调用,在 message 里告知 LLM "denied by user"
```

### 8.4 Pi 退出 + 恢复

```
pi 进程退出(SIGTERM、用户 S、panic、natural exit)
  │
  ▼
PTY Read 返回 EOF
  │
  ▼
PiClient.readLoop scanner.Scan() 返回 false
  │
  ▼
PiEvent{Type: "process_exit", ExitCode: 0}
  │
  ▼
Model 路由:
  - ticket.PiState = PiStateExited
  - EventPane 标记最后一条为 "⏹ exited"
  - 看板卡片状态变 "exited"(灰)
  │
  ▼ (用户想再跑)
按 s 键(同 ticket)
  │
  ▼
Model.spawnPi 检测 ticket.PiSpawnedAt != nil
  │  → opts.ContinueLast = true
  │  → PiClient.Start({... "--continue"})
  │
  ▼
pi 用上次的 session 续接(message 历史自动加载)
  │
  ▼
ticket.PiSessionID 保持(同一个 session 文件继续追加)
```

### 8.5 Awp 退出

```
按 q (有 pi 在跑)
  │
  ▼
ModeConfirm:
  ┌─ Quit ─────────────────────┐
  │  3 pi sessions running.    │
  │  Quit anyway?              │
  │  [y] Yes  [n] No           │
  └────────────────────────────┘
  │
  ▼
用户 y → Model.mode = ModeShuttingDown
  │
  ▼
Model.Cleanup():
  for ticket, pane := range m.panes {
      pane.Client.Stop()  ← SIGINT → 3s → SIGKILL
  }
  │
  ▼
tea.Quit
  │
  ▼
** pi 进程结束,但 session 文件保留在 ~/.pi/agent/sessions/ **
  │
  ▼
下次启动 awp:
  - awp session ls → 看到上次所有 session
  - awp session resume <id> → 加载该 session 到新 ticket
  - 或在 ticket 上按 s(自动 --continue 最近的)
```

---

## 9. CLI

```bash
# 1. 主入口:启动看板
awp                                          # 默认 project
awp --project /path/to/repo                  # 指定 project
awp --ticket 43                              # 跳到指定 ticket

# 2. 项目管理
awp project new [name]                       # 当前目录创建 project
awp project list                             # 列出所有 project
awp project delete <name-or-id>

# 3. Ticket 管理
awp ticket new [-p PROMPT] [-w WORKTREE]     # 非交互创建
awp ticket list [PROJECT]
awp ticket show <TICKET_ID>                  # 打印 ticket 详情
awp ticket remove <TICKET_ID>
awp ticket attach <TICKET_ID>                # 脱离看板 attach

# 4. Pi session 管理
awp session list [PROJECT]                   # 列出 pi sessions
awp session show <SESSION_ID>                # 打印 session 详情
awp session resume <SESSION_ID>              # 启动 awp 并恢复
awp session fork <SESSION_ID> [ENTRY_ID]     # 拉新 awp,fork 此 session
awp session export <SESSION_ID> [-o FILE]    # 导出 HTML

# 5. 配置
awp config generate                          # 生成默认配置
awp config validate                          # 校验
awp config path                              # 打印路径
awp config edit                              # $EDITOR 打开

# 6. 调试
awp doctor                                   # 检查 pi binary、配置、worktree
awp --debug                                  # verbose 日志
awp --no-extension                           # 不加载 interception extension
awp --no-interception                        # 同上(更明确)
awp version
```

---

## 10. 测试策略

### 10.1 单元测试

| 包 | 策略 |
|---|------|
| `board/`, `project/`, `git/`, `config/` | 纯逻辑,无 I/O,覆盖率 80%+ |
| `pi/session.go` | 写真假 `.jsonl` 文件,断言解析 |
| `pi/events.go` | 喂 JSON 字符串,断言 PiEvent 字段 |
| `pi/commands.go` | mock writer,断言序列化 |
| `ui/eventpane/` | 喂事件序列,断言渲染输出 |
| `ui/terminalpane/` | mock PTY 输出,断言 vt10x 解码与 View |
| `ui/model.go`(board 布局) | mock ticket,断言 renderBoard 布局 |
| `agent/status.go` | 喂 PiEvent 序列,断言 PiState 转换 |

### 10.2 集成测试(需要真 pi binary)

```go
//go:build integration

func TestPiClient_StartStop(t *testing.T) {
    if _, err := exec.LookPath("pi"); err != nil {
        t.Skip("pi binary not found")
    }
    c := NewPiClient(DefaultOptions)
    events := []PiEvent{}
    c.OnEvent(func(e PiEvent) { events = append(events, e) })

    require.NoError(t, c.Start(context.Background(), StartOptions{
        CWD:    t.TempDir(),
        Prompt: "echo hello and exit",
    }))

    require.Eventually(t, func() bool {
        return slices.ContainsFunc(events, func(e PiEvent) bool {
            return e.Type == "agent_end"
        })
    }, 30*time.Second, 100*time.Millisecond)

    require.NoError(t, c.Stop())
}

func TestPiClient_ResumedSession(t *testing.T) {
    // 1) 启 pi 跑一次 → 拿 session ID
    // 2) 停
    // 3) 用 --session 重启 → 验证 message_history 加载
}

func TestPiClient_ToolCallEvents(t *testing.T) {
    // 启 pi, prompt 让它跑 `echo test` bash
    // 验证 tool_execution_start / update / end 三事件齐全
    // 验证 args 解析正确
}

func TestPiClient_AbortMidStream(t *testing.T) {
    // 启 pi + 长 prompt
    // 中途 Abort
    // 验证 agent_end with willRetry=false
}
```

### 10.3 TUI 集成测试(`testutil`)

```go
// internal/testutil/pi.go
func (e *TestEnv) StartRealPi(t *testing.T, ticketID string) *PiClient {
    if _, err := exec.LookPath("pi"); err != nil {
        t.Skip("pi binary not found")
    }
    c := NewPiClient(...)
    c.OnEvent(...)  // 投递到 env 的 event buffer
    require.NoError(t, c.Start(...))
    e.RegisterCleanup(func() { c.Stop() })
    return c
}

func (e *TestEnv) WaitForPiState(t *testing.T, ticketID string, want PiState, timeout time.Duration) {
    require.Eventually(t, func() bool {
        return e.GetTicket(ticketID).PiState == want
    }, timeout, 50*time.Millisecond)
}
```

### 10.4 端到端 demo 测试

**`make demo`**:启动 awp,自动化 5 分钟的"创建 ticket → 启 pi → 跑任务 → 完成"流程,录 gif 展示。这是 ship 前的最后检查。

---

## 11. 实施路线图

### Phase 0:基础设施(2 周)

- [ ] 从早期多 agent kanban fork 项目骨架(已完成)
- [ ] 删多 agent 抽象(agent interface、AgentPriority、FindOpencodeSession 等)
- [ ] 跑通 `go build && go test` — 确认无回归
- [ ] 写一个"最小 awp":只 `awp project new` + `awp`(看板只有 list,无 pi)

**验收**:`awp` 能启动,能创建/列出 project 和 ticket,看板能渲染空状态。

### Phase 1:Pi 集成骨架(3 周)

- [ ] `internal/pi/client.go` — PTY 启动 + JSONL 解析
- [ ] `internal/pi/events.go` — PiEvent 类型
- [ ] `internal/pi/commands.go` — 30+ RPC 命令
- [ ] `internal/pi/session.go` — 扫描 `~/.pi/agent/sessions/`
- [ ] `internal/agent/pane.go` — PiPane 包装
- [ ] `internal/agent/status.go` — PiState 状态机
- [ ] 集成到看板:能 spawn pi、卡片显示状态

**验收**:能 `s` 启 pi,卡片 1s 内显示状态变化,事件流能看到 `agent_start`/`tool_execution_*`/`agent_end`。

### Phase 2:UI 完整(2 周)

- [ ] `internal/ui/model.go` 整复制(3,040 行) + 改 pi 事件 + 改 Mode dispatch
- [ ] `internal/ui/view.go` 整复制(1,930 行) + 改 agent → pi
- [ ] `internal/ui/eventpane/model.go` — 事件流渲染(新写 ~300 行)
- [ ] `internal/ui/terminalpane/model.go` — PTY pane 嵌入(新写 ~150 行)
- [ ] 主题系统
- [ ] bubbles 库集成(textinput / textarea / spinner)
- [ ] 鼠标支持(原模板已实现,确认可用)

**验收**:全部 13 种 Mode 工作,主题切换正常,鼠标支持,光标闪烁,事件漂亮打印。

### Phase 3:Session 生命周期(1 周)

- [x] `awp session list/show/resume/fork/export`
- [x] `ModeSessionPicker` 模态框(P 键在 TUI 中打开)
- [x] Session 文件解析、统计(ToolCount, FirstPrompt, LastAssistant)
- [x] SessionStore.FindByID(id) — 跨 cwd 目录查 session
- [x] Lifecycle 修复(spawnPi 用 m.ctx, shutdown() 优雅退出)
- [x] WorktreePath 派生(awp/<branch>)

**验收**:重启 awp 能看到上次 session,resume 后事件从断点续。

**剩余(推到 Phase 5)**:
- Resume 时 worktree 自动创建(目前需要 ticket 已有 WorktreePath)
- 跨项目的 session 索引
- Session tag 分类

### Phase 4:Interception(2 周,可选)

- [x] `internal/pi/extension/awp-extension.ts` 编写
- [x] 协议: pi RPC extension_ui_request/response(不用 Unix socket)
- [x] `ModeInterception` 模态框 + 队列
- [x] 白名单/黑名单配置解析
- [x] `awp interception status` CLI
- [ ] auto_approve_after_seconds 超时(留 Phase 5)

**验收**:`interception.enabled: true` 时,黑名单命令前弹模态框,用户决策后 pi 收到响应。

**超出原计划的修改**:
- 不用 Unix socket;extension 和 awp 通信走 pi 自己的 RPC 流
  (extension_ui_request 事件)
- 原因: pi 已经在 RPC 里 forward UI 请求;Unix socket 会增加新协议、跨平台问题
- auto_approve_after_seconds 推迟到 Phase 5(需要决定超时默认通过/拒绝)

### Phase 5:Polish(1-2 周,持续)

- [ ] 8 套主题
- [ ] `--debug` 日志
- [ ] `awp doctor`
- [ ] `awp --version`
- [ ] README + docs
- [ ] Demo gif
- [ ] Homebrew formula

**总估时**:9-12 周(全栈一人,make things done 估算)

---

## 12. 风险与缓解

| 风险 | 概率 | 影响 | 缓解 |
|------|------|------|------|
| pi 协议升级,RPC 字段变 | 中 | 高 | 锁 pi 版本范围;事件解析失败时降级到 PTY 模式;集成测试覆盖 |
| 多个 pi session 同跑,内存涨 | 中 | 中 | 老 session 自动归档;`max_concurrent_pis` 上限(默认 20) |
| Extension 加载慢或挂掉 | 低 | 中 | 异步加载;`--no-extension` 跳过;超时熔断 |
| PTY 启动阶段(loading)和 JSONL 切换有歧义 | 中 | 低 | 启发式:首字节是 `{` 才切;失败回退 |
| Pi session 文件被外部损坏 | 低 | 高 | 启动 validate;损坏提示用户选 recovery |
| 用户误用 interception 把 pi 卡死 | 中 | 中 | **默认关闭**;`auto_approve_after_seconds: 30` 兜底;UI 倒计时 |
| Worktree 创建/删除失败 | 低 | 中 | 重试 + 用户提示;不阻塞其他 ticket |
| pi binary 不在 PATH | 低 | 高 | `awp doctor` 检测;启动时硬检查 |

---

## 13. 设计决策(独立论证)

每个决策都基于**产品目标**和**现有约束**,不依赖与其他方案的比较。

### 13.1 为什么用 PTY 启动 RPC 模式?

**目标**:看到 pi 启动序列(loading、ASCII art、model 切换)+ 结构化事件流。

**约束**:pi `--mode rpc` 启动后是 JSONL,但启动前几秒是控制台输出。

**选择**:用 PTY 启动(覆盖启动序列),读到首字节是 `{` 时切到 JSONL 解析,之后的字节只走事件流。

**替代方案 1**:分离 stdout/err,只读 stdout 的 JSONL
- ✗ 启动序列输出在 stderr,丢失

**替代方案 2**:全部走 PTY,屏幕正则
- ✗ 实现复杂(见"为什么不用纯 PTY 路线")

### 13.2 为什么不用纯 PTY 路线(只读屏幕)?

**目标**:看到 pi 在做什么。

**约束**:纯 PTY 看不到结构化事件(只能匹配"看起来在跑 bash"等模糊信号)。

**选择**:PTY 启动 + JSONL 解析双轨。PTY pane 是"显示"(原汁原味),事件流是"理解"(精确)。

**替代方案**:只 PTY
- ✗ 工具调用 args 不可见(屏幕可能截断)
- ✗ 状态机不准确(屏幕匹配不可靠)
- ✗ 无法发 RPC 命令(只能模拟键盘)

### 13.3 为什么只支持 pi?

**目标**:深胜于广。

**约束**:每加一个 agent 抽象 = N 倍维护成本(N 个上游版本、N 套 session 格式、N 种状态机)。

**选择**:pi 是单一依赖,做透。其他 agent 用户用原多 agent kanban 模板。

**替代方案**:通用 Agent interface(像早期 fork 模板)
- ✓ 用户多
- ✗ 浅:每个 agent 只支持基础能力
- ✗ 难跟踪上游

### 13.4 为什么 Interception 默认关闭?

**目标**:不强制用户观点。开箱即用,高级功能 opt-in。

**约束**:
- pi 自身有 permission 系统(`--approve`、`ui.confirm`)
- interception 是**额外一层**,对相同问题给同样答案
- interception 有"卡死 pi"风险(用户离开/遗忘)

**选择**:`interception.enabled: false`。用户要开就显式开,自己负责"为什么需要第二层"。

### 13.5 为什么 Kanban(3 列)而不是 List?

**目标**:让用户对"在做什么、做完什么、待做什么"有空间感。

**约束**:列表(单列)也能管多 session,但缺少"完成感"。

**选择**:3 列(Backlog / In Progress / Done)+ Sidebar(项目)+ Detail(事件)。

**替代方案**:4 列(Backlog / Todo / In Progress / Done)— opencode 风格
- ✗ 列多=屏幕挤,中小屏(80x24)放不下
- ✓ 3 列更紧凑,符合终端

### 13.6 为什么 PTY Pane 是可选(默认 Events)?

**目标**:用户要的是"理解 pi 在做什么",不是"看 pi 屏幕"。

**约束**:PTY pane 显示 pi 原始 TUI,但对快速决策无用(还要读 screen)。

**选择**:默认 Events 视图(结构化,可搜索/可复制/可时间线),PTY 是补充(切换可见)。

### 13.7 为什么 Init Prompt 在命令行注入而非 session file?

**目标**:让 pi 知道"我正在为 ticket X 工作"。

**约束**:可写 `~/.pi/agent/sessions/{id}.jsonl` 的第一条 message,但需要读懂 pi 协议格式。

**选择**:`pi --name "ticket title" "你正在为 ticket X 工作..."` — 用 pi 自己的 `--name` 和命令行 prompt 注入。简单、可靠、跨 pi 版本兼容。

---

## 14. 开放问题

1. **session 文件冲突** — 两个 awp 实例同时读同一个 session 文件,会怎样?需要加文件锁?
   **倾向**:不加,文档警示"不要同时跑两个 awp 操作同一 project"
2. **Init prompt 长度** — 长 prompt 占用 token,要限制吗?
   **倾向**:500 token 上限,超出截断 + 省略号
3. **Auto-spawn on ticket create** — 创建 ticket 时自动启动 pi 吗?
   **倾向**:**不**。按 s 显式启。让用户有控制感
4. **多 pi 在同一 ticket** — 一个 ticket 能 fork 出多 session 并行吗?
   **倾向**:**不支持**。一 ticket 一 session。fork 由用户手动
5. **Session 归档** — 已 done 的 ticket,对应 session 文件保留还是清理?
   **倾向**:**保留**(用户可能在 awp 外也想看)
6. **Worktree 共享** — 多个 ticket 能共用 worktree 吗?
   **倾向**:**不能**(避免冲突)
7. **Theme editor** — 终端内编辑主题?
   **倾向**:**不做**,改 JSON 文件

---

## 15. 验收标准(Ship Gate)

### 15.1 Phase 1(3 周后)

- [ ] `awp` 能启动并显示空看板
- [ ] 创建 project + ticket,持久化到 JSON
- [ ] 在 ticket 上按 s,`pi` 子进程启动(--mode rpc)
- [ ] 1 秒内,卡片显示 `starting` → `idle`
- [ ] 给 pi 发 prompt,卡片变 `streaming` → `idle`
- [ ] pi 跑 `bash` 时,事件流出现 `tool_execution_start` 条目
- [ ] 停止 pi,卡片变 `exited`
- [ ] 退出 awp,pi 进程被清理
- [ ] `go test ./...` 全过

### 15.2 Phase 3(6 周后)

- [ ] 全部 13 种 Mode 工作
- [ ] 事件流显示 5+ 事件类型(agent_start/turn/message/tool/compaction)
- [ ] PTY pane 正常渲染 pi TUI
- [ ] `awp session list` 显示项目所有 session
- [ ] `awp session resume <id>` 加载指定 session
- [ ] `--continue` 续接最近 session 验证 message history 加载
- [ ] 8 套主题可切换

### 15.3 Phase 4 + Ship(10 周后)

- [ ] `interception.enabled: true` 触发黑名单拦截
- [ ] 模态框正常弹,用户决策后 pi 收到响应
- [ ] auto_approve_after_seconds 超时
- [ ] `awp doctor` 全通过
- [ ] Demo gif(< 5 分钟,展示全流程)
- [ ] README + 4 篇 docs 完成
- [ ] Homebrew tap 可用

---

## 16. 参考资料

### 设计参照(历史模板,仍可参考)

- [早期 fork 来源(多 agent kanban 项目)](https://github.com/TechDufus/openkanban) — PTY/TUI 架构范本(awp 起步模板)
- [早期 fork 项目 ARCHITECTURE.md](https://github.com/TechDufus/openkanban/blob/main/ARCHITECTURE.md)
- [pi-mono](https://github.com/badlogic/pi-mono) — pi 核心
- [pi-mono README](https://github.com/badlogic/pi-mono/blob/main/README.md)
- [Bubble Tea](https://github.com/charmbracelet/bubbletea) — TUI 框架
- [Lipgloss](https://github.com/charmbracelet/lipgloss) — 终端样式
- [bubbles](https://github.com/charmbracelet/bubbles) — textinput/textarea/spinner
- [creack/pty](https://github.com/creack/pty) — PTY 绑定
- [hinshun/vt10x](https://github.com/hinshun/vt10x) — 终端模拟器

### Pi 协议必读

- `pi --help` — CLI flags 全集
- `pi --mode rpc < /dev/null` — 看启动序列
- `cat ~/.pi/agent/sessions/<some>/<id>.jsonl` — 看 session 格式
- pi-mono 源码路径:
  - `packages/coding-agent/src/modes/rpc/rpc-types.ts` — RPC 协议类型
  - `packages/coding-agent/src/modes/rpc/rpc-mode.ts` — 协议实现
  - `packages/coding-agent/src/modes/rpc/rpc-client.ts` — 客户端封装
  - `packages/coding-agent/src/core/extensions/types.ts` — Extension hooks
  - `packages/coding-agent/src/core/agent-session.ts` — session 内部
  - `packages/coding-agent/src/core/session-manager.ts` — session I/O
  - `packages/agent/src/types.ts` — `AgentEvent` 类型

---

## 17. 文档维护

- 本文件是**唯一权威设计规格**
- 任何代码层变更若偏离本文件,需要先更新本文件
- 任何对产品形态的质疑,从本文件开始讨论
- 本文件不在 git 中被自动生成;它是 source of truth

---

**版本**:awp-design
**状态**:已就绪,等待用户确认 → 进入 Phase 0
**维护者**:awp contributors
