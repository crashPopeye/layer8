// Package plugin embeds the l8 Claude Code plugin that every session loads with --plugin-dir.
package plugin

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed .claude-plugin skills agents
var FS embed.FS

// Version identifies the embedded plugin's content.
func Version() string {
	h := sha256.New()
	fs.WalkDir(FS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := FS.ReadFile(path)
		h.Write([]byte(path + "\x00"))
		h.Write(b)
		return err
	})
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// Materialize writes the embedded plugin to root/<version> unless that folder
// exists, so in-place edits survive restarts, and returns the folder.
func Materialize(root string) (string, error) {
	dir := filepath.Join(root, Version())
	if _, err := os.Stat(dir); err == nil {
		return dir, nil
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp(root, ".tmp-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)
	if err := os.CopyFS(tmp, FS); err != nil {
		return "", err
	}
	return dir, os.Rename(tmp, dir)
}
