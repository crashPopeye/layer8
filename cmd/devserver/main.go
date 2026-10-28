// devserver serves the layer8 UI and API over plain HTTP on localhost, without
// the desktop shell, for UI development and browser-driven tests. It is never
// shipped in the app bundle. Live pushes are not available here; the UI falls
// back to polling.
package main

import (
	"flag"
	"log"
	"net"
	"net/http"

	"github.com/crashPopeye/layer8/internal/config"
	"github.com/crashPopeye/layer8/internal/core"
	"github.com/crashPopeye/layer8/internal/shellenv"
	"github.com/crashPopeye/layer8/ui"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:7999", "listen address (localhost only)")
	flag.Parse()
	host, _, err := net.SplitHostPort(*addr)
	if err != nil || (host != "127.0.0.1" && host != "localhost") {
		log.Fatal("devserver only listens on localhost")
	}
	shellenv.Fix()
	paths, err := config.ResolvePaths()
	if err != nil {
		log.Fatal(err)
	}
	app, err := core.New(core.Options{Paths: paths, Version: "dev"})
	if err != nil {
		log.Fatal(err)
	}
	defer app.Shutdown()
	app.Run()

	mux := http.NewServeMux()
	mux.Handle("/api/", app.Handler(core.Hooks{}))
	mux.Handle("/", http.FileServerFS(ui.FS))
	// Reject other Host headers so a web page can't reach the API via DNS rebinding.
	guard := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h, _, _ := net.SplitHostPort(r.Host)
		if h != "127.0.0.1" && h != "localhost" {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if o := r.Header.Get("Origin"); o != "" && o != "http://"+r.Host {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		mux.ServeHTTP(w, r)
	})
	log.Printf("layer8 devserver on http://%s (home %s)", *addr, paths.Home)
	log.Fatal(http.ListenAndServe(*addr, guard))
}
