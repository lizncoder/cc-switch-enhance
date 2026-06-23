// Package tray provides a minimal system tray (Wails v2 has none built in)
// using getlantern/systray (pure Go on Windows, no cgo).
package tray

import (
	"sync"

	"github.com/getlantern/systray"
)

// quitOnce makes Quit idempotent — it's invoked both from the tray menu's
// "退出" handler and from app shutdown, and systray.Quit panics on a second call.
var quitOnce sync.Once

// autoStartItem is the tray's "开机自启" item, stored so SetAutoStartState can
// toggle the checkmark after the backend changes the registry.
var autoStartItem *systray.MenuItem

// Controller is what the tray drives. The App implements it.
type Controller interface {
	ToggleShow()
	SetAlwaysOnTop(on bool)
	GetAutoStart() bool
	SetAutoStart(on bool)
	OpenCCSwitch()
	Quit()
}

// Run starts the tray. Blocks until systray.Quit is called; callers should
// run it in its own goroutine.
func Run(c Controller, icon []byte) {
	systray.Run(func() {
		systray.SetIcon(icon)
		systray.SetTitle("")
		systray.SetTooltip("cc-enhance")

		mToggle := systray.AddMenuItem("显示 / 隐藏", "显示或隐藏浮窗")
		mTop := systray.AddMenuItemCheckbox("窗口置顶", "切换始终置顶", true)
		mAutoStart := systray.AddMenuItemCheckbox("开机自启", "开机自动启动 cc-enhance", false)
		// Reflect the real registry state so the checkbox matches reality on launch.
		if c.GetAutoStart() {
			mAutoStart.Check()
		}
		mCCS := systray.AddMenuItem("打开 cc-switch", "打开 cc-switch 切换 provider")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 cc-enhance")

		autoStartItem = mAutoStart
		go func() {
			for {
				select {
				case <-mToggle.ClickedCh:
					c.ToggleShow()
				case <-mTop.ClickedCh:
					if mTop.Checked() {
						mTop.Uncheck()
						c.SetAlwaysOnTop(false)
					} else {
						mTop.Check()
						c.SetAlwaysOnTop(true)
					}
				case <-mAutoStart.ClickedCh:
					newState := !mAutoStart.Checked()
					c.SetAutoStart(newState)
					if newState {
						mAutoStart.Check()
					} else {
						mAutoStart.Uncheck()
					}
				case <-mCCS.ClickedCh:
					c.OpenCCSwitch()
				case <-mQuit.ClickedCh:
					c.Quit()
					systray.Quit()
					return
				}
			}
		}()
	}, nil)
}

// SetAutoStartState updates the tray menu checkbox without triggering the
// handler. Called by the App backend after SetAutoStart writes the registry.
func SetAutoStartState(on bool) {
	if autoStartItem == nil {
		return
	}
	if on {
		autoStartItem.Check()
	} else {
		autoStartItem.Uncheck()
	}
}

// Quit terminates the tray (call on app shutdown if needed).
func Quit() {
	quitOnce.Do(systray.Quit)
}
