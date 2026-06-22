# 额度预警（Quota Alert）设计

**日期**：2026-06-18
**分支**：cc-overlay-v2
**状态**：设计已确认，待实现

## 目标

cc-overlay 是常驻置顶的用量监控 overlay。当前只"显示"额度，用户需要主动看。
本功能让它在**额度接近上限时主动提醒**，防止超额断流（GLM）或余额耗尽（DeepSeek）。

## 判定逻辑（单档，超阈即预警）

给 snapshot 增加统一的预警状态 `warn: bool` + `warnReason: string`，后端计算，前端据
此变色 / 通知。

| Provider | 数据 | 预警条件 | 默认阈值 |
|----------|------|---------|---------|
| GLM | 5小时 / 7天 百分比 | 任一窗口 **≥ 阈值** | 85% |
| DeepSeek | 账户余额 | 余额 **≤ 阈值** | ¥10 |

两种 provider 的"危险"方向相反——GLM 是额度快满（百分比高），DeepSeek 是钱快花完
（余额低）——但都归一到同一个 `warn` 状态。预警原因（如 `"5h 87% · 7天 91%"` 或
`"余额 ¥6.2"`）传给前端用于通知文案。

阈值写进配置，即使单档也可在设置面板调整。

## 提醒机制 + 防骚扰

**系统通知**：用轻量库 `github.com/go-toast/toast`（底层调 Windows toast）。仅在
**首次进入预警**时发一次，文案如「GLM 7天额度 91%，接近上限」/「DeepSeek 余额 ¥6.2」。
点击通知聚焦到 overlay 窗口。

**UI 变红**（持续，直到脱离预警）：
- 卡片模式：呼吸边框从绿色（working）切换为**红色脉冲**；5h/7天 或余额那行数字变红加粗。
- 折叠栏：窄条边框变红，对应指标变红。
- 红色优先级高于"工作中绿色"——预警时即使模型在跑也显示红。

**防骚扰（滞后重置）**：进入预警 → 通知 1 次 + 持续红色。之后不会因百分比在阈值附近
抖动而反复弹通知。只有当指标**回落到安全线以下**（阈值 − `warnHysteresis`，默认即
85% − 10% = 75%）后预警状态才清零；下次再跨过 85% 才会再次通知。滞后带避免临界值刷屏。

## 数据结构

`internal/snapshot/types.go` 新增：
```go
Warn       bool   `json:"warn"`
WarnReason string `json:"warnReason"`
```

配置（`~/.cc-overlay/config.json`，`OverlayConfig` 新增）：
- `warnPercent int`（默认 85）— GLM 百分比阈值
- `warnBalance float64`（默认 10）— DeepSeek 余额阈值（¥）
- `warnHysteresis int`（默认 10）— 滞后带宽度，安全线 = 阈值 − 此值

前端类型（`frontend/src/types.ts`）镜像 `warn` / `warnReason`。

## 实现落点

- **预警计算**：放在 `buildSnapshot()`（每 1.5s，基于最新 `lastLimits` / `lastBalance`
  + 阈值算 `Warn` / `WarnReason`）。
- **通知去重**：App 结构体新增内存标志 `warned bool`：
  - `Warn==true && !warned` → 发 toast + `warned=true`
  - `Warn==false && warned` → `warned=false`（脱离滞后带重置）
  - 重启后 `warned` 归零；启动时若已预警会补通知一次（合理）。
- **前端**：`App.vue` 据 `snap.warn` 给 `.card` / `.bar` / `body` 加 `warn` class
  （红色脉冲 + 数字高亮）；`style.css` 加 `body.warn` 动画。
- **设置面板**：`App.vue` 设置区加 `warnPercent` / `warnBalance` 两个输入框，调
  `SetManualConfig` 同款的后端方法持久化。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/snapshot/types.go` | 新增 `Warn` / `WarnReason` |
| `internal/config/paths.go` | `OverlayConfig` 新增 3 个阈值字段 + 默认值 |
| `app.go` | `buildSnapshot` 算预警；`warned` 去重；toast 发送；新增保存阈值的方法 |
| `frontend/src/types.ts` | 镜像 `warn` / `warnReason` |
| `frontend/src/App.vue` | `warn` class 绑定、数字高亮、设置面板阈值输入 |
| `frontend/src/style.css` | `body.warn` 红色脉冲动画 |
| `go.mod` | 引入 `github.com/go-toast/toast` |

## 不在范围（YAGNI）

- 多档预警（黄/红/临界）— 先单档，需要再迭代。
- 用户自定义颜色 / 声音。
- 预警历史记录 / 统计（属于"数据与统计"方向，后续单独做）。
- macOS / Linux toast（当前仅 Windows；非 Windows 平台静默跳过通知，UI 变红仍生效）。
