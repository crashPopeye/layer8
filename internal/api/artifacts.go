package api

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/crashPopeye/layer8/internal/render"
	"github.com/crashPopeye/layer8/internal/task"
)

// maxArtifact bounds one artifact upload.
const maxArtifact = 16 << 20

// artifactView is an artifact with the URL that serves it to iframes and images.
type artifactView struct {
	task.Artifact
	URL string `json:"url"`
}

func (s *server) listArtifacts(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Task(r.PathValue("id"))
	if err != nil {
		reply(w, nil, err)
		return
	}
	artifacts, err := s.tasks.Artifacts(t.ID)
	views := make([]artifactView, len(artifacts))
	for i, a := range artifacts {
		views[i] = artifactView{Artifact: a, URL: s.artifactURL(t.ID)(a.Path)}
	}
	reply(w, views, err)
}

func (s *server) readArtifact(w http.ResponseWriter, r *http.Request) {
	data, err := s.tasks.ReadArtifact(r.PathValue("id"), r.PathValue("path"))
	if err != nil {
		reply(w, nil, err)
		return
	}
	serveArtifact(w, r, r.PathValue("path"), data)
}

func (s *server) writeArtifact(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxArtifact))
	if err != nil {
		writeError(w, http.StatusRequestEntityTooLarge, err.Error())
		return
	}
	a, err := s.tasks.WriteArtifact(r.PathValue("id"), r.PathValue("path"), data)
	reply(w, a, err)
}

func (s *server) renderArtifact(w http.ResponseWriter, r *http.Request) {
	t, err := s.store.Task(r.PathValue("id"))
	if err != nil {
		reply(w, nil, err)
		return
	}
	data, err := s.tasks.ReadArtifact(t.ID, r.PathValue("path"))
	if err != nil {
		reply(w, nil, err)
		return
	}
	out, err := render.Markdown(data, s.artifactURL(t.ID))
	reply(w, map[string]string{"html": string(out)}, err)
}

// sharedArtifact serves an artifact to the sandboxed iframes and images of the artifacts
// panel. They carry no cookie, so the URL itself holds a key derived from the token.
func (s *server) sharedArtifact(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hmac.Equal([]byte(r.PathValue("key")), []byte(s.artifactKey(id))) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	data, err := s.tasks.ReadArtifact(id, r.PathValue("path"))
	if err != nil {
		reply(w, nil, err)
		return
	}
	serveArtifact(w, r, r.PathValue("path"), data)
}

// artifactKey is the capability key of a task's shared artifact URLs.
func (s *server) artifactKey(taskID string) string {
	mac := hmac.New(sha256.New, []byte(s.token))
	mac.Write([]byte("artifact:" + taskID))
	return hex.EncodeToString(mac.Sum(nil))[:32]
}

// artifactURL maps paths of the task's artifacts folder to their shared URLs.
func (s *server) artifactURL(taskID string) render.URLFunc {
	base := "/artifact/" + taskID + "/" + s.artifactKey(taskID) + "/"
	return func(p string) string {
		segments := strings.Split(path.Clean(p), "/")
		for i, seg := range segments {
			segments[i] = url.PathEscape(seg)
		}
		return base + strings.Join(segments, "/")
	}
}

// serveArtifact writes an artifact's bytes. Opened on its own, HTML still runs sandboxed
// in an opaque origin, so it can never act with the user's cookie.
func serveArtifact(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	w.Header().Set("Content-Security-Policy", "sandbox "+render.Sandbox)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (s *server) listComments(w http.ResponseWriter, r *http.Request) {
	comments, err := s.tasks.Comments(r.PathValue("id"))
	reply(w, comments, err)
}

func (s *server) addComment(w http.ResponseWriter, r *http.Request) {
	var in task.CommentInput
	if !decode(w, r, &in) {
		return
	}
	c, err := s.tasks.AddComment(r.PathValue("id"), in)
	reply(w, c, err)
}

func (s *server) sendComments(w http.ResponseWriter, r *http.Request) {
	var in task.SendInput
	if !decode(w, r, &in) {
		return
	}
	n, err := s.tasks.SendComments(r.Context(), r.PathValue("id"), in, func(ctx context.Context, sessionID, text string) error {
		_, err := s.sessions.Send(ctx, sessionID, text)
		return err
	})
	reply(w, map[string]int{"sent": n}, err)
}
