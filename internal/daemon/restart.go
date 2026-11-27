package daemon

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"
)

// restarter stops the daemon once no session is busy, so launchd starts the binary now on disk.
type restarter struct {
	busy  func() bool
	stop  func()
	every time.Duration
	once  sync.Once
}

// ServeHTTP answers POST /api/restart with {"busy"}: whether the restart waits for sessions to finish.
func (rs *restarter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	busy := rs.busy()
	rs.once.Do(func() { go rs.wait() })
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]bool{"busy": busy})
}

func (rs *restarter) wait() {
	for rs.busy() {
		time.Sleep(rs.every)
	}
	rs.stop()
}
