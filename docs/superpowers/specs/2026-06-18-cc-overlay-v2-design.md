# cc-overlay v2 — 修复 + 实时图表 + 折叠长条

- 日期: 2026-06-18
- 状态: 已批准（待用户复核规格）
- 适用项目: `D:\codex-gen-pros`（模块 `cc-overlay`，Wails v2 + Vue3/TS）

---

## 1. 背景与问题

cc-overlay v1 已完成并验证：常驻置顶无边框浮窗，只读 cc switch 的 `cc-switch.db` + Claude Code 文件，监控当前 provider/模型 + 今日/本月/会话用量。但用户实测后发现四个问题：

1. **无关闭按钮** —— 卡片无边框，没有任何可见的关闭/最小化入口（只有托盘菜单）。
2. **数据不准确** —— 今日/本月总量、当前输入输出看起来偏小。
3. **没有实时输入输出图表** —— 只有静态数字，没有随时间变化的曲线。
4. **新需求：点击“关闭”应折叠成一个“高度只有一行、长条状”的数据框**，而非退出。

### 1.1 根因诊断（已对照真实库读数确认，2026-06-18）

用 `python` 只读直查 `C:\Users\<user>\.cc-switch\cc-switch.db`，与 v1 诊断输出对比：

| 指标 | v1 浮窗显示 | DB 真值 | 结论 |
|---|---|---|---|
| 今日 input_tokens | 1,304,011 | 1,317,825 | ≈一致（仅 ~1min 轮询滞后） |
| 今日 **cache_read_tokens** | 仅作小字次要字段 | **24,182,400** | 占真实上下文 ~95% |
| 今日真实上下文 (in+cr+cc) | — | **25,501,339** | v1 头条 1.3M 比真值小 ~20× |
| 本月 input_tokens | 28,595,608 | 28,609,422 | ≈一致 |
| 当前会话“最新输入” | **334** | 该轮 cache_read=81,664 → 上下文 ~82k | v1 只显示未命中缓存的新增 token |

**根因（用户已确认）：浮窗把“输入”显示成 `input_tokens`（仅新增、未命中缓存部分），而真实发给模型的上下文 = 新增 + 缓存读 + 缓存写。** 这同一个原因同时让今日/本月总量与当前输入输出都“看起来太小”。

补充发现：
- cc switch 自身面板用的 `usage_daily_rollups` **完全没有 6 月数据**（只有 1–5 月），所以若拿浮窗和 cc switch 面板对比会对不上 —— 但浮窗（读 live logs）才是对的。
- 今天有 **4 个活跃 claude 会话**（`ebcf52b3`、`4e771807`、`d0578aa3`、`a0ac331f`）；v1 “当前会话”只跟踪 sessions 注册表里 updatedAt 最新的那一个。
- 所有 claude 行都是 `data_source='session_log'`、`provider_id='_session'`（cc switch 纯回灌 Claude Code 会话日志，没有真实代理行）；`total_cost_usd` 今日为 0，本月有值（cc switch 在 ingestion 时按 `model_pricing` 算）。
- 当前“最新输入输出”在测试中比 DB 落后 2 条消息（v1 只用 transcript tail，1s 轮询窗口 + 可能的 glob 延迟）。

## 2. 目标 / 非目标

**目标**
1. “输入”头条反映真实上下文（含缓存读/写）。
2. 加入实时输入输出折线图（每条请求一个点）。
3. 加关闭（折叠）按钮：点击折叠成一行长条；长条可展开回完整卡片；退出仍走托盘。
4. 长条里展示：模型+状态点、今日输入/输出、实时输入/输出、迷你 sparkline。
5. 当前输入输出不再滞后。

**非目标（YAGNI）**
- 不做多窗口 / 多 app 各钉一张卡（仍是顶部单选切换）。
- 不接入远端 GLM 套餐额度（留作后续）。
- 不改 cc switch 或 Claude Code 的任何文件（只读约束不变）。
- 不引入图表第三方库（保持 exe 体积）。

## 3. 方案选型（已定：方案 1）

- **方案 1（采用）**：单窗口折叠 + DB 驱动时序。关闭=缩窗高+切换长条视图；图表/sparkline 数据来自 `proxy_request_logs` 的 `RecentRequests` 查询。
- 方案 2（双窗口）：Wails v2 多窗口开销大、需同步位置/置顶，收益不明显，弃。
- 方案 3（内存环形缓冲存时序）：重启即丢且与 DB 重复，弃。

## 4. 设计

### 4.1 准确性修正 —— “输入” = 总上下文

- 在快照类型上新增字段 `ContextTokens = InputTokens + CacheRead + CacheCreate`，保留原有分项字段。
- UI 头条 **“输入”** 显示 `ContextTokens`；分项（新增 / 缓存读 / 缓存写）降级为小字次要展示（或 tooltip）。
- 输出（output_tokens）含义不变。
- 覆盖：今日、本月、当前会话（会话总量 + 最新一条）、最近请求。
- 效果：今日 ~25.5M（原 1.3M）；当前 ~82k（原 334）。
- 纯 token 列求和展示，**不写 DB**。

### 4.2 实时输入输出 —— 永不滞后

- 实时值 = transcript tail usage 与 **当前 session 的最新 `proxy_request_logs` 行**二者中较新者（按时间戳比较）。
- 新增查询 `LatestSessionRequest(app, sessionID)`：`WHERE app_type=? AND session_id=? ORDER BY created_at DESC LIMIT 1`，返回 model/in/out/cr/cc/created_at。
- 既保留 transcript tail 的亚秒级响应，又保证 1.5s 内必然追上（DB 是统一真源）。
- 非 claude app 仍用全局最新请求行（现状）。

### 4.3 实时图表 + 迷你 sparkline —— DB 驱动时序

新增查询 `RecentRequests(app, since int64) []SeriesPoint`：
```sql
SELECT CAST(created_at AS INTEGER),
       CAST(COALESCE(input_tokens,0) AS INTEGER),
       CAST(COALESCE(output_tokens,0) AS INTEGER),
       CAST(COALESCE(cache_read_tokens,0) AS INTEGER),
       CAST(COALESCE(cache_creation_tokens,0) AS INTEGER)
FROM proxy_request_logs
WHERE app_type=? AND created_at>=?
ORDER BY created_at ASC
```
- **时间窗**：默认近 60 分钟（`since = now - 60min`）。今日 ~235 req/天 → 60min 内约 10–30 点，渲染平滑无需降采样；某窗口 >200 点时客户端等距抽稀。
- 快照携带 `Series []Point`，服务端预算每点 `In = in+cr+cc`，前端只渲染。
- **图表**（完整卡片内，header 下方独立区块）：约 64px 高、卡片宽度。输入（上下文）= 柔和蓝色填充 **area**（主轴，撑满高度）；输出 = 叠加的细亮色 **line**，用 **独立自缩放**（因输出约为输入的 1/30）。两个当前值均标注。X 轴=时间，自动滚动（最新在右）。**手写 SVG**，不引第三方库。
- **sparkline**（折叠长条）：同一 Series 抽稀到 ~24 点，极小内联 SVG，仅画输入（上下文）趋势。
- 缩放说明（写进 spec 避免歧义）：输入上下文比输出大约 20–40×，故必须双独立自缩放；两条当前值用标签标注，避免误读。

### 4.4 折叠长条 + 关闭按钮（单窗口）

- header 加 **折叠按钮 “—”**。
  - 点击 → 调新绑定 `SetCollapsed(true)`：Go 侧 `runtime.WindowSetSize(ctx, 440, 36)`（长条**比卡片更宽**以容纳全部内容），前端切换到长条视图，config 持久化 `collapsed=true`。
- 长条可拖拽（drag region）；点击 **“▢”**（或双击长条）→ `SetCollapsed(false)`：恢复 320×460，切回完整卡片，`collapsed=false` 持久化。
- **尺寸明确（消除歧义）**：展开=320×460；折叠长条=440×36。故 `main.go` 必须放宽 `MaxWidth`（见 §8）。长条内容若仍偏挤，按优先级省略 provider 名（只留模型）、缩短标签（“今日”→“今”、“实时”→“实”），保证 sparkline + 展开按钮始终可见。
- **退出仍只走托盘**（“退出”项）；折叠按钮绝不退出。
- 因窗口 frameless 无原生关闭按钮，这个 “—” 即“无关闭按钮”的解法。
- 长条布局（ASCII，约 36px 高）：
  ```
  [● busy] glm-5.2·Zhipu │ 今日 ▲25.5M ▼382k │ 实时 ▲82k ▼3.8k │ ▁▂▄▆▇▆▄ [▢]
  ```
  - 状态点（绿/琥珀/灰）+ 模型·provider + 今日输入/输出（总上下文）+ 实时输入/输出（最新一条上下文）+ 迷你 sparkline + 展开按钮。
- 重启后按 `collapsed` 恢复长条/卡片态。
- `MinHeight` 需允许 ~36px（frameless 可任意尺寸，确认 main.go 约束放宽）。

### 4.5 多会话

- 今日/本月总量跨所有 session（`WHERE app_type=?`），正确。
- “当前会话”= sessions 注册表里 updatedAt 最新者（现状不变）。
- 切 app 时长条/卡片均刷新到新 app。

## 5. 数据模型变更

`internal/snapshot/types.go`：
- `UsageTotals`、`SessionInfo`、`RequestInfo` 各加 `ContextTokens int64 \`json:"contextTokens"\``。
- 新增 `SeriesPoint { T int64; In int64; Out int64 }`（json: `t,in,out`）。
- `Snapshot` 加 `Series []SeriesPoint \`json:"series"\``。
- `Snapshot` 加 `Collapsed bool \`json:"collapsed"\``（供前端首屏确定视图，真正状态由 config 持久化）。

`internal/config`（OverlayConfig）：
- `Collapsed bool`
- `ChartWindowMin int`（默认 60）

## 6. 后端变更（`app.go` / `ccswitchdb`）

`internal/ccswitchdb/queries.go`：
- `RecentRequests(appType string, since int64) ([]SeriesPoint, error)`
- `LatestSessionRequest(appType, sessionID string) (*RequestRow, error)`

`app.go`：
- `buildSnapshot()`：对 today/month/session/latest 计算 `ContextTokens`；取实时值=transcript tail vs `LatestSessionRequest` 较新者；取 `RecentRequests(app, now-ChartWindowMin*60)` 填 `Series`（每点 In=in+cr+cc）。
- 新绑定 `SetCollapsed(on bool) error`：`cfg.Collapsed=on`；持久化；`runtime.WindowSetSize`（展开时 320×460，折叠时 W×36）；emit 一次快照。
- `restorePosition()` 旁加 `restoreCollapsed()`：startup 时若 `cfg.Collapsed` 则直接以长条尺寸显示。
- `toUsage`/`toRequest` 填 `ContextTokens`。

## 7. 前端变更

`frontend/src/types.ts`：镜像新字段（contextTokens、series、collapsed、SeriesPoint）。

`frontend/src/App.vue`：
- 头条“输入”绑定 `contextTokens`；分项小字。
- header 右侧加折叠按钮 `—`（调 `SetCollapsed(true)`）。
- 新增 `<ChartView :series>` 区块（header 下方）。
- 新增长条视图 `<div class="bar-mode">`：状态点+模型·provider+今日+实时+`<Sparkline :series>`+展开 `▢`（调 `SetCollapsed(false)`）。整条为 drag region。
- 根据 `snap.collapsed` 选择渲染卡片 or 长条。

`frontend/src/components/Chart.vue`（新）：手写 SVG，双独立自缩放 area+line，当前值标签。
`frontend/src/components/Sparkline.vue`（新）：手写 SVG，~24 点 polyline。

## 8. main.go

- 移除/放宽 `MaxWidth`、`MaxHeight`（frameless 浮窗不需要硬上限；折叠长条需 440 宽，超出原 `MaxWidth:360`）。`MinWidth`/`MinHeight` 设小（如 32）以允许长条尺寸。
- `Width:320, Height:460` 保持默认（展开态）。startup 时若 `cfg.Collapsed` 则 `restoreCollapsed()` 立即 `WindowSetSize(440,36)`。
- 其余（Frameless/AlwaysOnTop/背景色/CSSDrag/SingleInstanceLock/tray）不变。

## 9. 测试 / 验证

`diag_test.go` 扩展断言：
- `s.Today.ContextTokens == s.Today.InputTokens + CacheRead + CacheCreate`
- `len(s.Series) > 0`
- `s.Series[i].In == 该点 in+cr+cc`（抽查）
- 实时值非滞后（`LatestSessionRequest` 返回的 created_at ≥ transcript tail 时间 或 tail 为空时仍有效）

独立核对（python 只读）：
- `SELECT ... FROM proxy_request_logs WHERE app_type='claude' AND created_at>={now-3600}` 的点数与 `len(Series)` 一致；每点 in+cr+cc 与 Series.In 一致。
- 今日 `SUM(input)+SUM(cache_read)+SUM(cache_creation)` ≈ 浮窗头条。

手动（`wails dev`）：
1. 卡片出现，头条“今日输入”≈25M（不是 1.3M）。
2. 发一次请求 → 图表 ~1.5s 内新增点；sparkline 动。
3. 点 “—” → 缩成长条（~36px），内容齐全；点 “▢” → 恢复。
4. 折叠态重启 → 仍为长条；展开态重启 → 仍为卡片。
5. 托盘“退出”正常退出；折叠按钮绝不退出。
6. 切 app → 长条/卡片均刷新。

## 10. 边界 / 健壮性

- 近 60 分钟无请求 → 图表占位“近60分钟无请求”，sparkline 平直。
- DB 锁（cc switch 在写）→ 推送上次缓存快照 + `Errors` 项（现状）；Series 用上次缓存。
- 折叠态切 app → 长条更新到新 app。
- 单点窗口（Series 只有 1 点）→ 图表画一个点/平直线，不报错。
- 非 claude app 无 transcript → 实时值直接用全局最新请求行（现状），`LatestSessionRequest` 仅 claude 用。

## 11. 实现顺序（供 writing-plans 细化）

1. snapshot 类型加字段 + `ContextTokens` 计算 + diag 断言（先让数字变对，最低风险）。
2. `LatestSessionRequest` + 实时值取较新者。
3. `RecentRequests` + `Series` + diag 断言。
4. `Chart.vue` / `Sparkline.vue` 手写 SVG + 接入 App.vue。
5. 折叠按钮 + `SetCollapsed` + 长条视图 + 持久化 + main.go MinHeight。
6. 打磨 + `wails build` + 端到端验证。
