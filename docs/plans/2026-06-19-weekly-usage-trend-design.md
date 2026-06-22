# 近 7 天用量趋势（Weekly Usage Trend）设计

**日期**：2026-06-19
**分支**：cc-overlay-v2
**状态**：设计已确认，待实现

## 目标

在完整卡片里加一张近 7 天的每日 token 消耗柱状图，让用户一眼看出"今天 vs 过去几天"的起伏，而不只是看"今日"和"近 60 分钟"两个瞬时值。

## 数据来源（复用，不新增持久化）

cc-switch 的 SQLite 数据库本身保留约 30 天的请求日志（代码注释：`logs retain ~30 days`）。现有的 `TodayTotals(app, since, until)` 就是按时间区间从日志聚合的。所以 7 天柱状图复用同一查询——每天一个区间，调 7 次。

**不需要新增任何持久化文件**。今天的柱子是动态值（每 1.5s 更新），过去 6 天是历史定值。

## 数据结构

`internal/snapshot/types.go` 新增：
```go
WeeklyUsage []DayUsage `json:"weeklyUsage"`

type DayUsage struct {
    Date    string `json:"date"`    // "06-13" 短格式
    Tokens  int64  `json:"tokens"`  // 当天 context + output + cache 合计
    IsToday bool   `json:"isToday"` // 标记今天那根柱子
}
```

前端 `frontend/src/types.ts` 镜像。

## 后端查询

在 `buildSnapshot` 算完 today/month 之后，循环过去 7 天：
```go
weekly := make([]snapshot.DayUsage, 0, 7)
for i := 6; i >= 0; i-- {
    dayStart := startToday.AddDate(0, 0, -i)
    dayEnd := dayStart.AddDate(0, 0, 1)
    if t, err := a.db.TodayTotals(app, dayStart.Unix(), dayEnd.Unix()-1); err == nil {
        weekly = append(weekly, snapshot.DayUsage{
            Date:    dayStart.Format("01-02"),
            Tokens:  t.InputTokens + t.OutputTokens + t.CacheRead + t.CacheCreate,
            IsToday: i == 0,
        })
    }
}
s.WeeklyUsage = weekly
```

性能：7 次本地 sqlite 查询 × 每 1.5s 一次。本地查询 <1ms，可接受。先按简单方案；若实测有压力，再优化为"历史 6 天放 60s limitsLoop 算、今天放主 loop 实时算"。

## 前端

**新增组件** `frontend/src/components/WeeklyChart.vue`：轻量 SVG 柱状图（复用现有 Chart.vue 思路），7 根柱子。
- 柱高按 7 天最大值归一化
- 今天的柱子用亮蓝（动态跳动），过去 6 天用暗灰
- 悬停显示数值（SVG `<title>` 标签，不写复杂 tooltip）
- 高度约 40px，紧凑

接口：`<WeeklyChart :days="snap.weeklyUsage" />`

**布局**：放在"今日消耗"卡片下方、"实时"上方。折叠栏不加（空间不够，历史趋势是"想细看时展开看"的信息）。

## 涉及文件

| 文件 | 改动 |
|------|------|
| `internal/snapshot/types.go` | 新增 `DayUsage` + `Snapshot.WeeklyUsage` |
| `app.go` | `buildSnapshot` 加 7 天循环 |
| `frontend/src/types.ts` | 镜像 `DayUsage` + `weeklyUsage` |
| `frontend/src/components/WeeklyChart.vue` | **新建** SVG 柱状图 |
| `frontend/src/App.vue` | 引入 WeeklyChart，放在趋势图下方 |

## 不在范围（YAGNI）

- 30 天 / 月级趋势（需求是 7 天）
- 历史数据导出 CSV
- 折叠栏显示历史
- 跨 app 对比（一次只看当前选的 app）
