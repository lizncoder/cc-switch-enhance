# Quota Alert Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** 当 GLM 额度百分比超阈或 DeepSeek 余额过低时，cc-overlay 发一次 Windows toast 通知并持续红色脉冲，直到指标回落到安全线以下。

**Architecture:** 预警判定提取为 `internal/snapshot.EvaluateAlert` 纯函数（可单测，输入 limits/balance/阈值，输出 dangerous/safe/reason）。`app.go` 的 `buildSnapshot` 每次调用它，用一个内存 `warned` 标志做状态机：进入 dangerous 发一次 toast，回到 safe 才清零（滞后带防抖）。前端据 `snap.warn` 给 body/card/bar 加 `warn` class。

**Tech Stack:** Go (Wails v2)、Vue 3 + TS、`git.sr.ht/~jackmordaunt/go-toast/v2`（已在 go.mod indirect 依赖中）、标准 `testing`。

**Baseline:** `cc-overlay-v2` @ `f0b2b4f`（含 DeepSeek 余额 + per-provider UI）。

参考设计：`docs/plans/2026-06-18-quota-alert-design.md`。

---

## Task 1: 预警判定纯函数 `EvaluateAlert`（TDD）

**Files:**
- Create: `internal/snapshot/alert.go`
- Test: `internal/snapshot/alert_test.go`

**Step 1: 写失败测试**

`internal/snapshot/alert_test.go`:
```go
package snapshot

import "testing"

func TestEvaluateAlert(t *testing.T) {
	cases := []struct {
		name        string
		limits      []PlanLimit
		balance     *BalanceInfo
		percent     int
		balanceThr  float64
		hysteresis  int
		wantDanger  bool
		wantSafe    bool
		wantReason  string
	}{
		{
			name: "glm 7d over threshold",
			limits: []PlanLimit{{Window: "5小时", Percent: 80}, {Window: "7天", Percent: 90}},
			percent: 85, hysteresis: 10,
			wantDanger: true, wantSafe: false, wantReason: "7天 90%",
		},
		{
			name: "glm both windows over",
			limits: []PlanLimit{{Window: "5小时", Percent: 87}, {Window: "7天", Percent: 91}},
			percent: 85, hysteresis: 10,
			wantDanger: true, wantSafe: false, wantReason: "5小时 87% · 7天 91%",
		},
		{
			name: "glm safe below safe-line",
			limits: []PlanLimit{{Window: "5小时", Percent: 70}, {Window: "7天", Percent: 70}},
			percent: 85, hysteresis: 10,
			wantDanger: false, wantSafe: true, wantReason: "",
		},
		{
			name: "glm hysteresis band (neither)",
			limits: []PlanLimit{{Window: "5小时", Percent: 80}},
			percent: 85, hysteresis: 10,
			wantDanger: false, wantSafe: false, wantReason: "",
		},
		{
			name: "deepseek balance low",
			balance: &BalanceInfo{TotalBalance: "6.20", Currency: "CNY"},
			balanceThr: 10, hysteresis: 10,
			wantDanger: true, wantSafe: false, wantReason: "余额 ¥6.2",
		},
		{
			name: "deepseek balance healthy",
			balance: &BalanceInfo{TotalBalance: "50.00", Currency: "CNY"},
			balanceThr: 10, hysteresis: 10,
			wantDanger: false, wantSafe: true, wantReason: "",
		},
		{
			name: "no data",
			percent: 85, balanceThr: 10, hysteresis: 10,
			wantDanger: false, wantSafe: false, wantReason: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			danger, safe, reason := EvaluateAlert(c.limits, c.balance, c.percent, c.balanceThr, c.hysteresis)
			if danger != c.wantDanger || safe != c.wantSafe || reason != c.wantReason {
				t.Errorf("EvaluateAlert: danger=%v safe=%v reason=%q; want danger=%v safe=%v reason=%q",
					danger, safe, reason, c.wantDanger, c.wantSafe, c.wantReason)
			}
		})
	}
}
```

**Step 2: 跑测试确认失败**

Run: `go test ./internal/snapshot/ -run TestEvaluateAlert -v`
Expected: FAIL（`EvaluateAlert` undefined）

**Step 3: 实现 `EvaluateAlert`**

`internal/snapshot/alert.go`:
```go
package snapshot

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// EvaluateAlert decides the alert state from the latest quota data.
//   - dangerous: GLM any window percent >= percentThr, or DeepSeek balance <= balanceThr.
//   - safe:      GLM all windows <= percentThr - hysteresis, or DeepSeek balance >= balanceThr + hysteresis.
//   - reason:    human description of what crossed the line (empty when not dangerous).
//
// The band between (threshold - hysteresis) and threshold is the hysteresis
// zone: neither dangerous nor safe, so the caller's warned flag holds (no flapping).
func EvaluateAlert(limits []PlanLimit, balance *BalanceInfo, percentThr int, balanceThr, hysteresis float64) (dangerous, safe bool, reason string) {
	if len(limits) > 0 {
		safeLine := float64(percentThr) - hysteresis
		var over []string
		allSafe := true
		for _, p := range limits {
			pct := float64(p.Percent)
			if pct >= float64(percentThr) {
				over = append(over, fmt.Sprintf("%s %d%%", p.Window, p.Percent))
			}
			if pct > safeLine {
				allSafe = false
			}
		}
		// Stable order for deterministic reason text.
		sort.Strings(over)
		return len(over) > 0, allSafe && len(over) == 0, strings.Join(over, " · ")
	}
	if balance != nil {
		amt, _ := strconv.ParseFloat(balance.TotalBalance, 64)
		safeLine := balanceThr + hysteresis
		if amt <= balanceThr {
			return true, false, fmt.Sprintf("余额 ¥%s", trimAmount(balance.TotalBalance))
		}
		if amt >= safeLine {
			return false, true, ""
		}
		return false, false, "" // hysteresis band
	}
	return false, false, ""
}

// trimAmount drops a trailing ".00" for compact display ("6.20" -> "6.2").
func trimAmount(s string) string {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return s
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}
```

**Step 4: 跑测试确认通过**

Run: `go test ./internal/snapshot/ -run TestEvaluateAlert -v`
Expected: PASS（所有子用例）

**Step 5: Commit**

```bash
git add internal/snapshot/alert.go internal/snapshot/alert_test.go
git commit -m "feat: add EvaluateAlert quota-warning predicate (TDD)"
```

---

## Task 2: snapshot 加 `Warn` / `WarnReason` 字段

**Files:**
- Modify: `internal/snapshot/types.go`（Snapshot 结构体）
- Modify: `frontend/src/types.ts`（Snapshot 接口）
- Modify: `frontend/wailsjs/go/models.ts`（wails build 时自动重生成，无需手改）

**Step 1: 改 `internal/snapshot/types.go`**

在 `Snapshot` 结构体末尾（`Balance` 字段后）加：
```go
	Warn       bool   `json:"warn"`
	WarnReason string `json:"warnReason"`
```

**Step 2: 改 `frontend/src/types.ts`**

在 `Snapshot` 接口的 `balance?: BalanceInfo;` 后加：
```ts
  warn: boolean;
  warnReason: string;
```

**Step 3: 验证编译**

Run: `go build ./internal/snapshot/`
Expected: 无错误

**Step 4: Commit**

```bash
git add internal/snapshot/types.go frontend/src/types.ts
git commit -m "feat: add Warn/WarnReason to snapshot payload"
```

---

## Task 3: config 加阈值字段 + 默认值

**Files:**
- Modify: `internal/config/paths.go`（`OverlayConfig` 结构体 + `DefaultOverlayConfig`）

**Step 1: `OverlayConfig` 末尾加字段**

```go
	WarnPercent    int     `json:"warnPercent"`    // GLM percent threshold (default 85)
	WarnBalance    float64 `json:"warnBalance"`    // DeepSeek balance threshold in CNY (default 10)
	WarnHysteresis float64 `json:"warnHysteresis"` // hysteresis band width (default 10)
```

**Step 2: `DefaultOverlayConfig` 末尾加默认值**

```go
		WarnPercent:    85,
		WarnBalance:    10,
		WarnHysteresis: 10,
```

（`LoadOverlayConfig` 已有"合并进默认值"逻辑，新字段会自动有默认值，无需额外补全代码。）

**Step 3: 验证编译**

Run: `go build ./internal/config/`
Expected: 无错误

**Step 4: Commit**

```bash
git add internal/config/paths.go
git commit -m "feat: add warn threshold fields to overlay config"
```

---

## Task 4: app.go 集成预警 + `warned` 状态机 + toast

**Files:**
- Modify: `app.go`（App 结构体加 `warned bool`；`buildSnapshot` 调 `EvaluateAlert` + 状态机；新增 `sendToast` + `SetWarnThresholds` 方法）
- Modify: `go.mod`（`go-toast/v2` 由 indirect 提升；`go mod tidy`）

**Step 1: App 结构体加 `warned` 字段**

在 `App` 结构体（`lastBalance` 附近）加：
```go
	warned bool // true while a warning toast has been fired and not yet cleared
```

**Step 2: 新增 `sendToast` 封装**

`app.go` 顶部 import 加 `"git.sr.ht/~jackmordaunt/go-toast/v2"`（实现时确认该库 v2 的确切构造/方法名，通常是 `toast.Toast{Title, Body}.Activate()` 或 `toasts.New(title, body).Play()`——以实际为准）。新增方法：
```go
// sendToast fires a Windows toast. Best-effort: errors are logged, not fatal.
func sendToast(title, body string) {
	n := toast.Toast{Title: title, Body: body} // adjust to actual go-toast/v2 API
	if err := n.Activate(); err != nil {
		log.Printf("toast failed: %v", err)
	}
}
```
> 实现时跑一次最小 toast 确认能弹（Windows toast 需 AppID；go-toast/v2 默认用可执行名作 AppID，通常可直接显示。若不弹，按 go-toast/v2 README 设置 `Shortcut`/`AppID`）。

**Step 3: `buildSnapshot` 末尾算预警 + 状态机**

在 `buildSnapshot` 返回前（`s.Collapsed = a.cfg.Collapsed` 之后、`return s` 之前）加：
```go
	// Quota warning: evaluate current state, drive the warned state machine.
	danger, safe, reason := snapshot.EvaluateAlert(s.PlanLimits, s.Balance,
		a.cfg.WarnPercent, a.cfg.WarnBalance, a.cfg.WarnHysteresis)
	s.Warn = danger
	s.WarnReason = reason
	if danger && !a.warned {
		a.warned = true
		title := "额度预警"
		body := "cc-overlay：" + reason
		if reason == "" {
			body = "cc-overlay：额度接近上限"
		}
		go sendToast(title, body)
	} else if safe && a.warned {
		a.warned = false // fell below the safe line; re-arm for next crossing
	}
```
> 注意：`a.warned` 仅在 `buildSnapshot` 里读写，且 `buildSnapshot` 由 `emit()` 串行调用（loop / cc.Updated / limitsLoop），无需加锁。`a.cfg` 同样在主路径读，安全。

**Step 4: 新增 `SetWarnThresholds` 后端方法（供设置面板调用）**

```go
// SetWarnThresholds persists GLM percent / DeepSeek balance warn thresholds.
func (a *App) SetWarnThresholds(percent int, balance float64) error {
	if percent > 0 && percent < 100 {
		a.cfg.WarnPercent = percent
	}
	if balance >= 0 {
		a.cfg.WarnBalance = balance
	}
	_ = a.cfg.Save(a.paths.OverlayConfig, a.paths.OverlayDir)
	a.warned = false // re-evaluate immediately with new thresholds
	go func() {
		a.fetchLimits()
		a.emit()
	}()
	return nil
}
```

**Step 5: 重新生成 wails 绑定 + tidy + 编译**

Run:
```bash
go mod tidy
go build ./...
```
Expected: 无错误（`go-toast/v2` 变 direct，绑定后续由 wails build 生成）

**Step 6: Commit**

```bash
git add app.go go.mod go.sum
git commit -m "feat: wire quota warning into buildSnapshot + toast"
```

---

## Task 5: 前端预警 UI（红色 class + 数字高亮）

**Files:**
- Modify: `frontend/src/App.vue`（watch `snap.warn` → toggle body class；warn 时数字高亮）
- Modify: `frontend/src/style.css`（`body.warn` 红色脉冲）

**Step 1: `style.css` 加红色脉冲（与绿色 working 同结构，优先级更高）**

在 `body.working` 动画后加：
```css
body.warn {
  animation: breathe-warn 1.6s ease-in-out infinite;
}
@keyframes breathe-warn {
  0%, 100% { box-shadow: inset 0 0 0 1px #5a1e1e, inset 0 0 12px rgba(245,80,80,0.25); }
  50%      { box-shadow: inset 0 0 0 1px #f55050, inset 0 0 24px rgba(245,80,80,0.65); }
}
```
> 注意：预警红压过工作绿。`App.vue` 的 `watch(working)` 和 `watch(warn)` 都 toggle body class；两者可并存，CSS 后定义的 `body.warn` 视觉占优（同 animation 名不冲突，因为是两个不同 class 同时存在时都触发各自动画——为避免冲突，在 warn 时移除 working class，见 Step 2）。

**Step 2: `App.vue` 加 `warn` 状态 + body class 互斥**

script 区加 computed + watch：
```ts
const warn = computed(() => !!snap.value?.warn);
// warn (red) takes priority over working (green): only apply working when not warning.
watch([working, warn], ([w, wa]) => {
  document.body.classList.toggle('working', !!w && !wa);
  document.body.classList.toggle('warn', !!wa);
}, { immediate: true });
```
> 替换/补充之前的 `watch(working, ...)`（第 4 段加的那个）——改为这个组合 watch，保证 warn 时移除 working。

**Step 3: 关键数字高亮（warn 时变红）**

模板里，完整卡片的 stats 行和 plan/balance 区的数字加 `:class="{ red: warn }"`。例如 5h/7天 span：
```html
<span v-if="isGLM" :class="{ red: warn }"><i>5h</i>{{ stat5h }}</span>
<span v-if="isGLM" :class="{ red: warn }"><i>7天</i>{{ stat7d }}</span>
```
`.plan-pct`（百分比）、`.b-metric`（折叠栏）同理加 `:class="{ red: warn }"`。

scoped CSS 加：
```css
.red { color: #f55050 !important; }
```

**Step 4: 构建前端验证编译**

Run: `cd frontend && npm run build`
Expected: vue-tsc + vite build 成功

**Step 5: Commit**

```bash
git add frontend/src/App.vue frontend/src/style.css
git commit -m "feat: red pulse + number highlight when snap.warn"
```

---

## Task 6: 设置面板加阈值输入

**Files:**
- Modify: `frontend/src/App.vue`（settings 表单 + 调 `SetWarnThresholds`）
- Modify: `frontend/wailsjs/go/main/App.js` + `.d.ts`（wails build 自动生成 `SetWarnThresholds` 绑定）

**Step 1: App.vue import 加 `SetWarnThresholds`**

```ts
import { GetSnapshot, ListApps, SetApp, SetCollapsed, SetBarWidth, GetManualConfig, SetManualConfig, ListProviders, SetWarnThresholds } from '../wailsjs/go/main/App';
```

**Step 2: settings 区加两个 ref + 表单字段**

script 区：
```ts
const cfgWarnPct = ref(85);
const cfgWarnBal = ref(10);
```
`openSettings()` 里（在 `GetManualConfig` Promise 旁）加读取（需后端 `GetManualConfig` 返回阈值，或新增 `GetWarnThresholds`——为最小改动，扩展 `openSettings` 从一次 `GetSnapshot` 读 `cfg.WarnPercent`，或直接用默认值。**推荐**：Task 4 里给 `GetManualConfig` 顺带返回阈值，避免新增方法）。

> 简化决策：在 `ManualConfig` 结构体加 `WarnPercent int` / `WarnBalance float64`，`GetManualConfig` 一并返回，`SetManualConfig` 不动；阈值用单独的 `SetWarnThresholds` 存。`openSettings` 读 `ManualConfig` 填表单。

settings 模板（Token 输入框后、hint 前）加：
```html
<label>GLM 预警阈值 (%)</label>
<input v-model.number="cfgWarnPct" type="number" min="1" max="99" spellcheck="false" />
<label>DeepSeek 预警余额 (¥)</label>
<input v-model.number="cfgWarnBal" type="number" min="0" step="0.5" spellcheck="false" />
```

`saveCfg()` 里在 `SetManualConfig` 后加：
```ts
try { await SetWarnThresholds(cfgWarnPct.value, cfgWarnBal.value); } catch (e) { console.error('set warn failed', e); }
```

**Step 3: 后端 `ManualConfig` 加阈值字段（Task 4 之外的小补充，归入此 task）**

`app.go` 的 `ManualConfig`：
```go
type ManualConfig struct {
	BaseURL     string  `json:"baseUrl"`
	Token       string  `json:"token"`
	WarnPercent int     `json:"warnPercent"`
	WarnBalance float64 `json:"warnBalance"`
}
```
`GetManualConfig` 填上 `WarnPercent: a.cfg.WarnPercent, WarnBalance: a.cfg.WarnBalance`。

**Step 4: Commit**

```bash
git add app.go frontend/src/App.vue
git commit -m "feat: warn threshold inputs in settings panel"
```

---

## Task 7: wails build + 端到端验证

**Step 1: 完整构建**

Run:
```bash
wails build
```
Expected: `Built '...\build\bin\cc-overlay.exe'`，wails 自动重生成 `frontend/wailsjs/go/main/App.js`（含 `SetWarnThresholds`）和 `models.ts`（含 `Warn`/`WarnReason`）。

**Step 2: 临时调低阈值触发预警**

改 `~/.cc-overlay/config.json`（或用设置面板）把当前 provider 的阈值调到必触发：
- GLM 模式：`"warnPercent": 1`（任何百分比都超）
- DeepSeek 模式：`"warnBalance": 999999`（余额必低于）

**Step 3: 启动验证**

Run: `./build/bin/cc-overlay.exe`
Expected：
- 窗口边框红色脉冲（非绿色）
- 首次启动弹一条 Windows toast「额度预警」
- 5h/7天 或余额数字变红

**Step 4: 验证防骚扰滞后**

把阈值调回正常（`warnPercent: 85`），观察：指标若仍 >85%，**不再弹 toast**（只保持红）。再把阈值调到使指标 <75%（安全线），红色消失；再调回触发，应**再弹一次** toast。

**Step 5: 恢复合理默认**

把 `config.json` 阈值恢复 `warnPercent: 85` / `warnBalance: 10`。

**Step 6: Commit（绑定 + dist 产物如有变化）**

```bash
git add frontend/wailsjs/  # 绑定重生成
git commit -m "chore: regenerate wails bindings for quota alert" --allow-empty
```

---

## Done criteria

- [x] `go test ./internal/snapshot/ -run TestEvaluateAlert` 通过
- [x] GLM 超阈 → 红 + toast（一次）；DeepSeek 余额低 → 同
- [x] 滞后带：临界抖动不反复弹 toast
- [x] 设置面板可调两个阈值
- [x] 红色压过工作绿色
- [x] `wails build` 成功，exe 正常运行

## 不在范围（YAGNI）

- 多档预警、自定义颜色/声音、预警历史、macOS/Linux toast（非 Windows 静默跳过通知，UI 变红仍生效）。
