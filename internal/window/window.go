//go:build darwin

// Package window is layer8's macOS shell: one webview showing the daemon's UI,
// a menu-bar item badged with the sessions waiting for approval, and native
// notifications when a session needs the user.
package window

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/client"
	"github.com/crashPopeye/layer8/internal/daemon"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"github.com/wailsapp/wails/v3/pkg/icons"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

// Run makes sure the daemon for home and addr runs this version, then shows the
// window until the user quits. Closing the window only hides it.
func Run(home, addr, version string) error {
	agent, err := daemon.NewAgent(home, addr)
	if err != nil {
		return err
	}
	if err := agent.Ensure(context.Background(), version); err != nil {
		return err
	}
	token, err := os.ReadFile(client.TokenPath(home))
	if err != nil {
		return err
	}
	c := client.New("http://"+addr, strings.TrimSpace(string(token)))

	var notifier *notifications.NotificationService
	var services []application.Service
	if bundled() {
		notifier = notifications.New()
		services = append(services, application.NewService(notifier))
	}
	var win *application.WebviewWindow
	show := func() {
		win.Show()
		win.Focus()
	}
	app := application.New(application.Options{
		Name:     "layer8",
		Services: services,
		Mac:      application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "dev.layer8.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { show() },
		},
	})
	win = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:            "layer8",
		Width:            1440,
		Height:           900,
		MinWidth:         900,
		MinHeight:        560,
		BackgroundColour: application.NewRGB(0x19, 0x17, 0x24),
		URL:              "http://" + addr + "/auth?t=" + strings.TrimSpace(string(token)),
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		win.Hide()
		e.Cancel()
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) { show() })

	tray := app.SystemTray.New()
	tray.SetTemplateIcon(icons.SystrayMacTemplate)
	menu := app.NewMenu()
	menu.Add("Show layer8").OnClick(func(*application.Context) { show() })
	menu.AddSeparator()
	menu.Add("Quit layer8").OnClick(func(*application.Context) { app.Quit() })
	tray.SetMenu(menu)
	tray.OnClick(show)

	w := watcher{client: c, badge: tray.SetLabel, focused: win.IsFocused}
	if notifier != nil {
		notifier.OnNotificationResponse(func(notifications.NotificationResult) { show() })
		w.notify = func(title, body string) {
			notifier.SendNotification(notifications.NotificationOptions{ID: strconv.FormatInt(time.Now().UnixNano(), 10), Title: title, Body: body})
		}
	}
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if notifier != nil {
			go notifier.RequestNotificationAuthorization()
		}
		go w.run(app.Context())
	})
	return app.Run()
}

// bundled reports whether this executable runs from inside an .app, which macOS notifications require.
func bundled() bool {
	exe, err := os.Executable()
	return err == nil && strings.Contains(filepath.ToSlash(exe), ".app/Contents/MacOS/")
}
