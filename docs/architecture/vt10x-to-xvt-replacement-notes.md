# vt10x → x/vt 替换 — 实测笔记

**日期**: 2026-06-22
**分支**: `refactor/vt10x-to-xvt`
**状态**: ✅ 编译过,**所有测试通过**(包括 scrollback 集成测试)

---

## 替换内容

| 概念 | vt10x | x/vt |
|------|-------|------|
| Emulator 接口 | `vt10x.Terminal` | `*vt.Emulator` |
| Cell 类型 | `vt10x.Glyph{Char, Mode, FG, BG Color}` | `*uv.Cell{Content, Style, ...}` |
| 读 Cell | `t.Cell(x, y) Glyph` | `e.CellAt(x, y) *uv.Cell` |
| 写 Cell | `t.Write(data)` | `e.Write(data)` |
| 读尺寸 | `t.Size() (w, h int)` | `e.Width()`, `e.Height()` (int) |
| Resize | `t.Resize(w, h)` | `e.Resize(w, h)` |
| 锁 | `t.Lock()/Unlock()` 手动 | SafeEmulator 自动(本项目仍用 p.mu) |
| **Scrollback** | awp 自己实现的 `ScrollbackBuffer` | **`vt.ScrollbackLen()` + `vt.ScrollbackCellAt(x, y)`** |

## 关键适配

### 1. Cell 类型差异

**vt10x.Glyph** 是值类型,直接比较:
```go
glyph := p.vt.Cell(col, 0)
if glyph.Char == 0 { glyph.Char = ' ' }
if glyph.FG != currentFG { ... }
```

**uv.Cell** 是指针类型,可能 OOB 返回 nil,内容在 `Content string`:
```go
cell := p.vt.CellAt(col, row)  // may be nil for OOB
ch := cellRune(cell)            // helper returns ' ' for nil
cellStyle := cell.Style         // Style struct, not FG/BG fields
```

### 2. 死锁避免 — AltScreen callback

x/vt 的 `AltScreen` callback 在 `vt.Write()` 内部**同步触发**。
`awp.handleOutput()` 持 `p.mu` 调 `vt.Write`,callback 也想拿 `p.mu` → **死锁**。

**解决方案**:callback 不直接动 `p.altScreenActive`,而是发到 channel:

```go
p.altScreenActiveCh = make(chan altScreenEvent, 8)
p.vt.SetCallbacks(vt.Callbacks{
    AltScreen: func(active bool) {
        select {
        case p.altScreenActiveCh <- altScreenEvent{active: active}:
        default: // drop if buffer full
        }
    },
})
// 后台 goroutine drain channel 并在 p.mu 内应用更新
go p.altScreenConsumer()
```

**调用约定**:`StartCmd` 自动在 `Start()` 后同步调用 `installCallbacks()`;
直接调 `Start()` 的人需要自己调。

### 3. Scrollback — 用 x/vt 的,不要重复造轮子

最初的实现保留了 awp 自己的 `ScrollbackBuffer`,但实测发现:

- x/vt 内部已经维护 Scrollback(默认 10000 行)
- awp 的 `captureScrollbackBeforeWrite/AfterWrite` 比较 `CellAt(0, 0)` 内容变化
- x/vt 的 `CellAt()` 对未修改的 cell 返回相同指针(快照一致性)
- 导致 `cellContentEqual` 永远返回 true → changed=false → 不 push

**修复**:删除 awp 自己的 `ScrollbackBuffer`,直接代理到 x/vt 的:
- `Pane.ScrollbackLen()` → `e.ScrollbackLen()`
- `Pane.GetScrollbackLine(i)` → 拼接 `e.ScrollbackCellAt(0..width, i)` 的 Content
- `selection` 的 scrollback 数据也直接从 x/vt 取

**好处**:
- 代码量大幅减少(scrollback.go 88 行 + 200 行测试删除)
- 行为正确(x/vt 内部 `Screen.DeleteLine()` 已经把行 push 到 Scrollback)
- 不再需要 `lastTopRow` snapshot 机制

### 4. 颜色 / 样式

- `vt10x.Color` 是 uint32(`< 0x01000000` 为特殊值)
- `uv.Cell.Style` 使用 `image/color.Color` 接口

新 helper `colorToANSI(c color.Color, isFG bool) string` 处理转换:
- nil → ""
- RGBA() → `38;2;r;g;b` 或 `48;2;r;g;b`
- 完全透明(a==0) → ""

新 `buildANSIFromStyle(style uv.Style) string` 直接从 uv.Style 构建 ANSI 序列。

### 5. 删除了什么

| 删除项 | 行数 | 原因 |
|--------|------|------|
| `vt10x.WithWriter` 调用 | - | x/vt 不需要 |
| `noopWriter` 类型 | 4 | x/vt 不需要 |
| `ScrollbackBuffer` 类型 + 测试 | 88+200 | 用 x/vt 自己的 |
| `captureScrollbackBeforeWrite` | 22 | 不需要 — x/vt 自动 push |
| `captureScrollbackAfterWrite` | 25 | 同上 |
| `isLineVisible` | 18 | 同上 |
| `Pane.scrollback` 字段 | 1 | 同上 |
| `Pane.lastTopRow` 字段 | 1 | 同上 |

## 已知限制

### 1. `scrollbackSize` 参数目前仅 informational

awp 的 `Pane.New(id, w, h, scrollbackSize)` 仍接受 scrollbackSize 参数,
但 x/vt 的 `Emulator.ScrollbackSize` 默认 10000,没在 `Start()` 中配置。
`Pane.ScrollbackSize()` 返回的是入参值,不代表实际生效大小。

**未来工作**:在 `installCallbacks()` 中调 `e.SetScrollbackSize(p.scrollbackSize)`。

### 2. `installCallbacks()` 仅在 `StartCmd` 调用

如果调用方用 `Start()` 而非 `StartCmd()`,callback 不会安装,alt-screen 检测失败。
**当前缓解**:ui/model.go 用 `StartCmd`,所有生产路径 OK。

### 3. Cell 指针语义

x/vt 的 `CellAt()` 返回 `*uv.Cell` 指针,如果 buffer 未变,可能返回相同指针。
**已缓解**:`cellContentEqual()` 用 Content+Style 比较替代指针比较。

## 测试状态

| 类别 | 状态 |
|------|------|
| 编译 | ✅ PASS |
| vet | ✅ PASS |
| `go test ./...` | ✅ PASS (10 个包) |
| `go test -race ./internal/terminal/` | ✅ PASS |
| `go test -tags integration ./test/...` | ✅ PASS (8 个测试,包括 scrollback) |
| `go test -tags e2e ./e2e/...` | ✅ PASS |

## 测试方法

```bash
# 基础测试
go test ./internal/terminal/ -count=1 -v

# 集成测试(scrollback + spawn)
go test -tags integration ./test/terminal/ -v

# race
go test -race ./internal/terminal/ -count=1 -v

# e2e
go test -tags e2e ./e2e/... -count=1
```
