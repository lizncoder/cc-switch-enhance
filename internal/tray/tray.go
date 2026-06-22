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

// Controller is what the tray drives. The App implements it.
type Controller interface {
	ToggleShow()
	SetAlwaysOnTop(on bool)
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
		mCCS := systray.AddMenuItem("打开 cc-switch", "打开 cc-switch 切换 provider")
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("退出", "退出 cc-enhance")

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

// Quit terminates the tray (call on app shutdown if needed).
func Quit() {
	quitOnce.Do(systray.Quit)
}
