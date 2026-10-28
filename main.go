// layer8 is a macOS workbench for running many long agent tasks in parallel.
package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/core"
	"github.com/crashPopeye/layer8/internal/shellenv"
	"github.com/crashPopeye/layer8/ui"
)

// version is set at build time: -ldflags "-X main.version=0.1.0".
var version = "dev"

func main() {
	// Finder-launched apps get a minimal PATH; claude, git, gh and tmux need the user's.
	shellenv.Fix()

	paths, err := config.ResolvePaths()
	if err != nil {
		log.Fatal(err)
	}
	if f, err := os.OpenFile(filepath.Join(paths.Logs, "layer8.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
	}
	log.Printf("layer8 %s starting", version)

	var app *application.App
	l8, err := core.New(core.Options{
		Paths:   paths,
		Version: version,
		Emit: func(name string, data any) {
			if app != nil {
				app.Event.Emit(name, data)
			}
		},
	})
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/api/", l8.Handler(core.Hooks{
		PickFolder: func() (string, error) {
			return app.Dialog.OpenFile().
				SetTitle("Choose a workspace folder").
				CanChooseDirectories(true).
				CanChooseFiles(false).
				PromptForSingleSelection()
		},
		OpenURL: func(u string) error { return app.Browser.OpenURL(u) },
		Quit:    func() { app.Quit() },
	}))
	mux.Handle("/", application.AssetFileServerFS(ui.FS))

	app = application.New(application.Options{
		Name:        "layer8",
		Description: "Parallel agent workbench",
		Assets:      application.AssetOptions{Handler: mux, DisableLogging: true},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "dev.layer8.app",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if w, ok := app.Window.GetByName("main"); ok {
					w.Show()
					w.Focus()
				}
			},
		},
		OnShutdown: l8.Shutdown,
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "layer8",
		Width:            1500,
		Height:           940,
		MinWidth:         1100,
		MinHeight:        640,
		URL:              "/",
		BackgroundColour: application.NewRGB(14, 13, 11),
		Mac: application.MacWindow{
			TitleBar: application.MacTitleBarHiddenInset,
		},
	})
	// Closing the window hides it; agents keep working. Quit with ⌘Q.
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		win.Hide()
		e.Cancel()
	})
	app.Event.OnApplicationEvent(events.Mac.ApplicationShouldHandleReopen, func(*application.ApplicationEvent) {
		win.Show()
		win.Focus()
	})

	l8.Run()
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
