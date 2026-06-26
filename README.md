# awp — agentic-with-pi

> 给人类开发者的快速指南。**awp** 是个 TUI 看板,
> 让你同时跑多个 [pi](https://github.com/earendil-works/pi-coding-agent) 会话,
> 每个工单一个 git worktree,从同一个界面观察和操控一切。

```
┌──────────────────┬──────────────────────────────────────────┐
│ Projects         │  Backlog  │ In Progress │ Done           │
│  awp             │  tkt-1    │  Resume ..  │ tkt-3          │
│                  │  tkt-2    │  [pi: run]  │                │
└──────────────────┴──────────────────────────────────────────┘
```

## 这是什么 / 不是什么

| 这是 | 这不是 |
|------|--------|
| 一个 TUI 看板 | IDE / 编辑器 |
| pi 的多任务编排器 | 多 agent 抽象层(只支持 pi) |
| 实时显示 pi 的运行状态 | 离线任务系统 |
| 基于 pi 的 `--mode rpc` 协议 | 基于屏幕正则的轮询 |

设计哲学:**深胜于广**。只把 pi 这一个 agent 做透,不做通用化。

## 30 秒上手

```bash
# 1. 装(任选一种)
go install github.com/pi/awp@latest         # 源码

# 2. 检查环境
awp doctor                                  # 7 项自检

# 3. 注册一个项目(在 git 仓库目录里运行)
cd ~/你的项目
awp project new myproject

# 4. 开 TUI
awp
# j/k 选工单 → s 启动 pi → Enter 看事件流 → P 选已有会话
```

需要 `pi` 在 `$PATH` 上。

## 常用命令

```bash
awp                              # 启动 TUI

awp project new [name]           # 注册当前目录为项目
awp project list                 # 列出项目
awp ticket list                  # 列出所有工单

awp session list .               # 列出 pi 会话
awp session show <id>            # 查看会话详情
awp session export <id> -f html  # 导出会话(HTML/Markdown)

awp interception status          # 查看拦截配置
awp doctor [--fix]               # 自检
awp theme list                   # 20 个主题
awp theme set dracula
awp version                      # 版本 + commit + 构建日期
awp --debug ...                  # 详细日志
```

## TUI 键位

| 键 | 动作 | 键 | 动作 |
|----|------|----|------|
| `j/k` | 上下选 | `n` | 新建工单 |
| `h/l` | 切列 | `s` | 启动 pi |
| `Enter` | 看事件流 | `S` | 停止 pi |
| `P` | 选历史会话 | `?` | 帮助 |
| `q` | 退出 | | |

拦截弹窗:`Y` 批准 / `N` 拒绝 / `A` 永久允许 / `Esc` 取消。

## 拦截(可选,默认关闭)

**警告**:开了之后,pi 的每一次工具调用都要你点头。

编辑 `~/.config/awp/interception.json`:

```json
{
  "enabled": true,
  "block_patterns": ["rm -rf /*", "sudo *", "* /etc/passwd"],
  "allow_patterns": ["ls *", "cat *", "pwd"]
}
```

简单 glob,不是正则。`allow_patterns` 先匹配(同时匹配两边算允许)。旧的 `blacklist`/`whitelist` 字段还能用(向后兼容)。

## 架构(给想读代码的人)

- **Bubble Tea** 写 TUI(单一 Model + 8 模式状态机)
- **creack/pty** 跑 pi 子进程(不用 tmux)
- **pi `--mode rpc`** 收 JSONL 事件(不抓屏幕)
- **cobra** 写 CLI
- **Lip Gloss** 做样式(20 主题)

```
cmd/awp/                 # cobra CLI(7 类子命令)
internal/
  pi/                    # RPC 客户端 + 30+ 命令 + 21 事件 + awp-extension.ts
  agent/                 # PiPane + 状态机
  ui/                    # TUI(8 modes, 9 个 per-concern 文件)
  app/                   # RunPanes + 导出(XSS-safe)
  config/                # 配置 + 主题 + 拦截
  doctor/                # 7 项自检
  git/                   # worktree 管理
  project/               # 项目注册 + 工单存储
  board/                 # Ticket + PiState
  terminal/              # PTY + vt10x
  observability/         # 调试日志(log/slog)
  buildinfo/             # ldflags 注入版本
test/{sub}/              # 集成测试(//go:build integration),子目录与 internal/ 镜像
e2e/                     # 端到端测试(//go:build e2e),平级,需真 awp + pi 二进制
```

设计细节看 `SYSTEM_DESIGN.md`(1551 行 spec)。
逐项 spec 对照看 `CROSS_VALIDATION.md`。

## 怎么开发

```bash
# 构建
make build                 # 本地调试版
make release VERSION=0.2.0 # 带版本号的发布版
# 相当于:
go build -ldflags="-s -w \
  -X github.com/pi/awp/internal/buildinfo.Version=0.2.0 \
  -X github.com/pi/awp/internal/buildinfo.Commit=$(git rev-parse --short HEAD) \
  -X github.com/pi/awp/internal/buildinfo.BuildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  -o awp .

# 测试
go test ./...                                    # 单元测试
go test -race ./...                              # 竞态检测
go test -tags integration ./test/...             # 集成测试(test/{sub}/,子目录镜像 internal/)
go test -tags e2e ./e2e/...                      # 端到端测试(平级,需真二进制)
go test -cover ./...                             # 覆盖率
go vet ./...                                     # 静态检查

# TypeScript 扩展(拦截)
cd internal/pi/extension && bun test
```

## 改代码前必读

1. **`AGENTS.md`** — 140 行,设计原则 + 黄金法则(PTY / 只支持 pi / 协议优于字节流)
2. **`SYSTEM_DESIGN.md`** — 完整设计
3. 命名要表达意图,函数职责单一(不发明接口)
4. `Update()` 永不阻塞,异步走 `tea.Cmd`
5. 改设计先改文档,再写代码
6. TDD:红 → 绿 → 重构,不跳步

## 质量现状

| 指标 | 值 |
|------|----|
| 审计分 | **94/100**(0 critical, 0 major) |
| 测试 | **335** 个(200 单元 + 19 集成 + 116 其他) |
| 覆盖率 | **63.4%** 平均(8 个包 > 50%) |
| 竞态检测 | clean |
| `go vet` | 0 warning |
| 二进制 | 14.9MB |

## 路线

6 个阶段全部完成:基础 → pi 协议 → TUI → 会话 → 拦截 → 硬化。

下一版(可选)待办:

- 5 个 deferred UI 模式(Settings / About / CommandPalette / ThemePicker / Worktree)
- auto_approve_after_seconds(超时自动批准)
- 真集成测试(需要 pi 二进制)

## 贡献

提 issue 之前先看 `AGENTS.md`。提交前自查:

```bash
go build -o awp . && go vet ./... && go test ./... && go test -race ./internal/...
```

Commit message 用 Conventional Commits,不加 AI 署名。

## License

MIT
