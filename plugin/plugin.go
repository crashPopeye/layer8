// Package plugin embeds the layer8 Claude Code plugin and installs it for Claude Code and Codex.
package plugin

import (
	"embed"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Files is the plugin: .claude-plugin/plugin.json and skills/<name>/SKILL.md.
//
//go:embed .claude-plugin skills
var Files embed.FS

// Install replaces dir with the embedded plugin.
func Install(dir string) error {
	if err := os.RemoveAll(dir); err != nil {
		return err
	}
	return os.CopyFS(dir, Files)
}

// MirrorCodex replaces the layer8 skills in skillsDir with the skills of the plugin in src,
// each renamed layer8-<name> because Codex has no plugin namespaces.
func MirrorCodex(src fs.FS, skillsDir string) error {
	old, _ := filepath.Glob(filepath.Join(skillsDir, "layer8-*"))
	for _, dir := range append(old, filepath.Join(skillsDir, "layer8")) {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return fs.WalkDir(src, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		name, rest, _ := strings.Cut(strings.TrimPrefix(p, "skills/"), "/")
		data, err := fs.ReadFile(src, p)
		if err != nil {
			return err
		}
		if path.Base(p) == "SKILL.md" {
			data = []byte(strings.Replace(string(data), "\nname: "+name+"\n", "\nname: "+CodexName(name)+"\n", 1))
		}
		dst := filepath.Join(skillsDir, CodexName(name), rest)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

// CodexName is the name a plugin skill gets in Codex: layer8-<name>, or layer8 for the layer8 skill itself.
func CodexName(skill string) string {
	if skill == "layer8" {
		return skill
	}
	return "layer8-" + skill
}
