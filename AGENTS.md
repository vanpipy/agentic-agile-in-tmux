# AGENTS.md

> 帮 AI 编码 agent **设计和执行任务**的指南。
> 不是项目计划,不是当前状态。原则、规则、框架。

---

## 1. 项目本质

`awp` 是**专为 pi**服务的 TUI 多任务协作看板:同时跑 N 个 pi session,统一界面看/控/切。
**只支持 pi 一种 agent**,不抽象。

设计哲学:**深胜于广** — pi 的每个 capability 都要有对应 UI,不服务其他 agent。
实现路径:从早期 fork 的多 agent kanban 模板起步,深度集成 pi 的 RPC 协议。awp 已
超出原模板范围,现在是独立项目,配置目录也独立为 `~/.config/awp/`。

---

## 2. 设计原则(决策时用)

| 原则 | 含义 |
|------|------|
| **协议优于字节流** | 用 pi 的 `--mode rpc` JSONL,不用屏幕正则 |
| **复制胜于发明** | 早期 fork 模板已 ship,不要重写 |
| **消费者不修改** | pi 是黑盒,只用 --mode rpc + extension,不 fork |
| **显式优于隐式** | 状态来自事件,不用"猜"(如"用户可能想关") |
| **可选优于强制** | interception 默认关,用户显式启用才生效 |
| **明确边界** | 一 ticket 一 session,一 worktree;不强求并行 |
| **plan-then-build** | `SYSTEM_DESIGN.md` 是 single source of truth,改设计先改文档 |
| **TDD 不妥协** | RED-GREEN-REFACTOR 不可跳步,无测试不提交 |
| **完备性优于简单** | 系统设计必须完备,不允许"先做个简化版"再"以后补"——以后不会补,只会留坑。复制就是复制(全部),不要 cherry-pick 容易的部分。|

---

### 2.1 严禁走捷径(强制)

**反面清单** — 这些话一出现在脑子里,立刻停下来:

| 借口 | 为什么禁 |
|------|----------|
| "**先做个简化版,以后再扩展**" | 简化版 = 永久版。后续接入要重写架构,成本 10×。 |
| "**只复制 terminal.Pane 就行,UI 太长**" | UI 才是用户看到的产品。漏 UI = 漏核心。 |
| "**这个功能 90% 用不上,先跳过**" | 不知道哪个 10% 用户会用到。漏的就是他卡死的那个。 |
| "**先让 build 过,适配以后再说**" | build 过 ≠ 工作。stub/unsafe-cast 是技术债炸弹。 |
| "**先跑通主流程,边角以后补**" | 边角 = 实际场景。你不知道哪个是边角。 |
| "**5.2 节描述说 3040 行,实际我们 500 行就够**" | 你不知道那 2540 行为什么存在。少一行 = 少一个未触发的 bug。 |
| "**早期模板引用了 A/B/C,我们没 A/B/C,所以删**" | 它的 A/B/C 是它花了 N 个月证明要有的。你 0 个月就敢删? |

**铁律**:
1. **系统设计必须完备**。缺哪块就补哪块,不许用"先省略"逃避。
2. **复制就是全部复制**。原模板 3040+1934 行,该几行就几行,不许"精简版"。
3. **stub/简化/unsafe-cast 算实现债**,提交时必须在 commit message 写 `DEBT:` 标注。
4. **拿不准到底该不该简化 → 问人**,不许自己拍板"先这样"。
5. **发现已经在走捷径 → 立刻停下** + `git reset --hard HEAD` + 重新规划。

**判断标准**: "如果用户问'你是不是按 X 实现的',我能 100% 答'是'吗?" 答"是简化版" = 失败。

---

## 3. 最佳实践(执行时用)

### 3.1 代码

- 命名表达意图,`pkg.Func` 读完知道做什么
- 错误 `wrap` 加上下文:`fmt.Errorf("ctx: %w", err)`,不丢 sentinel
- 公开 API 有 godoc(小写开头的不需要)
- 函数职责单一,能拆就拆
- 强类型优于 `interface{}`(Go 不用 any 除非真的需要)
- 不用 panic 处理可恢复错误
- `Update()` 永不阻塞 — 阻塞 = 整个 TUI 卡死

### 3.2 测试

- **RED → GREEN → REFACTOR** 不可跳
- 命名 `TestXxx_Scenario_Expected`(例:`TestDecide_BlacklistedCommand_Suspend`)
- 一个测试一个断言,失败信息明确
- 集成测试加 `//go:build integration` tag
- 关键模块跑 race:`go test -race ./internal/pi ./internal/agent`
- 覆盖目标:核心逻辑 80%+

### 3.3 提交

- Conventional Commits:`feat:` `fix:` `refactor:` `docs:` `test:` `chore:`
- 破坏性:`feat!: ...`(冒号前加 `!`)
- **不加 AI 署名**(`Co-authored-by: ...` 之类)— 仓库历史保持人类
- commit message 解释 **why**,不是 what(diff 自己会说话)

### 3.4 设计偏离

- 任何偏离 `SYSTEM_DESIGN.md` 的决策,先更新文档,再写代码
- 写一段 markdown 解释 "为什么选 A 不选 B"(附在 PR/issue)
- 拿不准就停下问,不要猜

---

## 4. 黄金法则(必须遵守,无例外)

1. **PTY,不用 tmux** — pi 在 PTY 里,原模板已证可行
2. **用 pi 的 RPC 协议,不自造** — `--mode rpc` JSONL 是事实标准
3. **拦截用 pi extension,不用 SIGSTOP** — `ui.confirm` 是官方机制
4. **只支持 pi** — 不做多 agent 抽象,不抽象 Agent interface
5. **不修改 pi 源码** — pi 是黑盒,只消费
6. **`Update()` 不阻塞** — 所有 I/O 走 `tea.Cmd`,在 goroutine 里跑
7. **改设计先改文档** — `SYSTEM_DESIGN.md` 是真理之源,代码不脱离文档
8. **不复刻不简化** — 原模板怎么写就怎么写,行数对齐,stub 标注 DEBT
9. **CORRECT 评估每个单元测试** — 提交前过 7 维检查,任一 ❌/⚠️ 不许合入:
   - **C**onformance:测符合规约?输出 = 期望字面值(不是"非空")
   - **O**rdering:处理顺序依赖?map 顺序未定义,**不依赖"第二个元素"**
   - **R**ange:边界(0/1/最大/超长/负数)?
   - **R**eference:外部依赖(文件/网络/时间)正确处理?
   - **E**xistence:空/nil/不存在路径?
   - **C**ardinality:0/1/N 个?
   - **T**ime:超时/竞态/过期?

   SKIP 必须配 TODO + 引 issue,**SKIP 不算覆盖**。

---

## 5. 技术框架(需要时用)

### 5.1 pi(被集成的对象)

- `pi --mode rpc` 走 JSONL:stdin 收命令,stdout 发事件
- session 文件在 `~/.pi/agent/sessions/{encoded-cwd}/*.jsonl`
- 工具名固定:`bash` `read` `edit` `write` `find` `grep` `ls`
- extension 钩子(实际名,直接用):`tool_call` `tool_result` `user_bash` `input` `confirm` `select` `agent_start` `agent_end` 等
- 事件流:agent_start/turn/message/tool_execution/compaction/auto_retry/queue_update
- 关键源:`pi-mono/packages/coding-agent/src/modes/rpc/rpc-types.ts`

### 5.2 架构模板(早期 fork 自多 agent kanban 项目)

- **UI 全在 `internal/ui/model.go`(3040 行)+ `view.go`(1934 行),无 sub-package**
- PTY 终端在 `internal/terminal/`(~1,250 行:pty + vt10x + scrollback + selection)
- 数据模型在 `internal/board/`(Ticket/Column,~160 行)
- 主题在 `internal/config/theme.go`(8 套预设)
- 模式:单 Model + 13 Mode 状态机。`Update()` 顶部 switch (msg),`View()` 顶部 if mode,调 `renderXxx()`
- 启动:tea.NewProgram(model, tea.WithAltScreen())
- 关键源:`internal/{ui,terminal,board,config}/`(已 fork 到 awp 名下)

**复刻规则**(已沉淀在仓库内,无需再 fork):
- import 全部为 `github.com/pi/awp/internal/...`
- 行数差 ≤ 100 (允许加 3-5 个 awp 独有 mode 常量 + awp 独有 Model 字段)
- 行数差 > 100 → 立刻停下,自查漏了什么

### 5.3 Bubble Tea(TEA 模式)

- `Update(msg) (tea.Model, tea.Cmd)` — 纯函数,只改 model state;异步返回 `tea.Cmd`
- `View() string` — 只渲染 model state,无副作用
- `tea.Cmd` 是闭包,runtime 在 goroutine 跑,结果以 `tea.Msg` 回 Update
- 自循环模式:每个 `OutputMsg` 触发下一次 `readOutput()`,直到 EOF
- render 节流:`tea.Tick(50ms, ...)` 避免每字节都重绘
- 子组件组合:独立 `tea.Model` 包装为 sub-Model(例:`ui/eventpane/model.go`)

### 5.4 Go

- 强类型优于 `interface{}`
- error 必须 wrap,带 context
- goroutine + channel 用于并发;不滥用 mutex
- 标准库优先,加第三方包前确认必要
- `go vet ./...` 必须 0 告警

### 5.5 Terminal 包(PTY + vt10x)

PTY management and terminal emulation for agent processes.

#### Core Components

- **Pane** - manages single PTY + virtual terminal
- **ScrollbackBuffer** - ring buffer for history (default 10k lines)
- **SelectionState** - text selection state machine

#### PTY Handling

Uses `creack/pty`:
```go
pty.Start(cmd)      // spawn with PTY
pty.Setsize(f, ws)  // resize
```

#### Terminal Emulation

Uses `vt10x` for escape sequence parsing:
- Cursor management
- Cell-based rendering
- Color/attribute handling

#### Message Types

BubbleTea integration:
- `OutputMsg` - new terminal output
- `ExitMsg` - process terminated
- `RenderTickMsg` - throttled render trigger

#### Rendering

- Throttled at 50ms intervals
- `dirty` flag tracks when re-render needed
- Cached view string until dirty

#### Key Translation

`translateKey()` converts BubbleTea `KeyMsg` to PTY bytes:
- Arrow keys → escape sequences
- Ctrl+C → 0x03
- Enter → \r

#### Environment

`buildCleanEnv()`:
- Sets `TERM=xterm-256color`
- Strips agent-related env vars
- Preserves PATH, HOME, USER

#### Escape Sequence Detection

Byte scanning for mode switches:
- Mouse mode: `\x1b[?1000h`
- Alt screen: `\x1b[?1049h`

#### Anti-Patterns

- Don't write to PTY without checking if alive
- Don't skip resize handling - causes display corruption
- Don't render on every output - use throttling
- Don't leak PTY file descriptors - always close
- Don't assume vt10x handles all sequences - some need manual parsing

---

## 6. 拿不准时(5 级 escalation)

1. **设计** → 翻 `SYSTEM_DESIGN.md`(单一事实之源)
2. **协议** → 翻 `pi-mono` 源码(以源码为准,不信博客)
3. **架构** → 翻 `internal/ui/` `internal/terminal/` 源码(它怎么做我们就怎么做,除非有明确理由)
4. **规范** → 翻 `.specify/memory/constitution.md`(TDD / godoc / vet)
5. **都没答案** → 写 issue-style markdown 描述问题,**先停下问人**,不要猜
6. **发现自己想"简化" → 翻 AGENTS.md §2.1**,确认是否违反"完备性优于简单"

---

## 7. 验证(每次提交前必须)

```bash
go build -o awp .                # 通过
go vet ./...                     # 0 告警
go test ./...                    # 全过
go test -race ./internal/pi ./internal/agent   # race 检测
```

提交前自查:以上 4 条全过 + commit message 解释 why。
