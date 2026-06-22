# cc-enhance

实时显示 Claude Code / GLM / DeepSeek / OpenCode 用量的桌面浮窗。常驻屏幕一角，置顶显示，每 1.5 秒刷新。

[English](#english) · [中文](#中文)

<p>
  <img src="https://img.shields.io/badge/platform-Windows-blue" alt="Windows">
  <img src="https://img.shields.io/github/v/release/lizncoder/cc-enhance?label=version" alt="version">
  <img src="https://img.shields.io/badge/license-MIT-green" alt="MIT">
</p>

---

<a id="中文"></a>

## 中文

我平时用 Claude Code，配 cc-switch 切 provider（GLM 套餐、DeepSeek、OpenCode Go 之类）。每次想知道今天用了多少 token、套餐还剩多少，都得打开 cc-switch 或各家的后台去看，很烦。所以写了这个小浮窗——常驻在屏幕一角，随时扫一眼就知道。

### 能干什么

- 今日 token 消耗（context + output，按 app）
- 最近 60 分钟的用量曲线
- 最新一次请求的输入输出
- 会话状态（忙 / 闲 + 时长）
- GLM 套餐：5 小时、7 天 百分比 + 重置倒计时
- DeepSeek：账户余额（总额 / 赠送 / 充值）
- OpenCode Go：各模型（deepseek / glm / kimi）调用次数，折叠栏里循环显示
- 额度超阈值时窗口变红，弹一次系统通知（默认 GLM 85%、DeepSeek 余额 ¥10，可在设置里改）
- 双击折叠成窄条，不挡视线
- 托盘菜单：显示/隐藏、置顶切换、一键打开 cc-switch

### 怎么用

1. 装好 [cc-switch](https://github.com/farion1231/cc-switch)，把 provider 配好
2. 从 [Releases](https://github.com/lizncoder/cc-enhance/releases/latest) 下 `cc-enhance.exe`
3. 双击运行。第一次启动会在 `~/.cc-enhance/` 自动生成配置

不用手动配 token，cc-enhance 直接读 cc-switch 的数据库，跟随你当前选的 provider。

> Windows 11 自带 WebView2。Windows 10 需要装一下：[下载](https://go.microsoft.com/fwlink/p/?LinkId=2124703)

### 设置

点右上角齿轮：可以填预警阈值（百分比 / 余额），Base URL 和 Token 一般不用动。

### 自己编译

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
git clone https://github.com/lizncoder/cc-enhance.git
cd cc-enhance
cd frontend && npm install && cd ..
wails dev      # 开发
wails build    # 出 build/bin/cc-enhance.exe
```

需要 Go 1.25+、Node.js 22+。

### 关于 Key

所有 API Key 都在运行时从 cc-switch 的本地数据库读取，代码里没有硬编码，编译出来的 exe 也不含任何密钥。对外请求只有查套餐和余额那几个 API。

---

<a id="english"></a>

## English

I use Claude Code with cc-switch to jump between providers (GLM plans, DeepSeek, OpenCode Go, etc.). Checking today's token usage or how much plan is left always meant opening cc-switch or each provider's dashboard — annoying. So I wrote this little overlay. It sits in a corner of your screen, always on top, refreshing every 1.5 seconds.

### What it does

- Today's token usage per app (context + output)
- 60-minute usage trend
- Latest request's input / output tokens
- Session status (busy / idle + duration)
- GLM plan: 5h and 7d percentage bars + reset countdown
- DeepSeek: account balance (total / granted / topped-up)
- OpenCode Go: per-model call counts (deepseek / glm / kimi), cycling in the collapsed bar
- Window turns red + fires one Windows toast when you cross the threshold (defaults: GLM 85%, DeepSeek ¥10 — configurable)
- Double-click to collapse into a slim bar
- Tray menu: show/hide, toggle always-on-top, open cc-switch

### Usage

1. Set up [cc-switch](https://github.com/farion1231/cc-switch) and configure your providers
2. Download `cc-enhance.exe` from the [Releases page](https://github.com/lizncoder/cc-enhance/releases/latest)
3. Run it. First launch auto-creates `~/.cc-enhance/config.json`

No manual token config needed — cc-enhance reads cc-switch's local DB and follows whatever provider you've selected.

> WebView2 is built into Windows 11. On Windows 10, install it: [download](https://go.microsoft.com/fwlink/p/?LinkId=2124703)

### Settings

Click the gear icon: set warn thresholds (percentage / balance). Base URL and Token usually don't need touching.

### Build from source

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
git clone https://github.com/lizncoder/cc-enhance.git
cd cc-enhance
cd frontend && npm install && cd ..
wails dev      # development
wails build    # produces build/bin/cc-enhance.exe
```

Requires Go 1.25+ and Node.js 22+.

### About keys

All API keys are read at runtime from cc-switch's local database. Nothing is hardcoded, and the compiled binary contains no secrets. Outbound requests are limited to the quota and balance APIs.

## License

MIT
