// Package updater keeps layer8 current from GitHub Releases.
//
// Trust model: releases are read with the user's GitHub token from the
// (private) repository, so only people with write access can publish one. The
// download must match the release's checksums.txt, and must pass
// `codesign --verify --deep --strict`. When the running app is signed with a
// Developer ID, the update must be signed by the same Team ID.
package updater

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/mod/semver"

	"github.com/crashPopeye/layer8/internal/github"
)

// Set at build time with -ldflags -X.
var (
	Owner = "crashPopeye"
	Repo  = "layer8"
)

const checksumsName = "checksums.txt"

type Status struct {
	Current     string `json:"current"`
	Latest      string `json:"latest"`
	Available   bool   `json:"available"`
	Notes       string `json:"notes"`
	URL         string `json:"url"`
	State       string `json:"state"` // idle, checking, downloading, installing, ready, error
	Error       string `json:"error"`
	CheckedAt   string `json:"checkedAt"`
	CanInstall  bool   `json:"canInstall"`
	InstallHint string `json:"installHint"`
}

type Updater struct {
	current string
	token   func() (string, error)

	mu      sync.Mutex
	status  Status
	release *github.Release
}

func New(current string, token func() (string, error)) *Updater {
	u := &Updater{current: current, token: token}
	u.status = Status{Current: current, State: "idle"}
	return u
}

func (u *Updater) Status() Status {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := u.status
	bundle, err := BundlePath()
	s.CanInstall = err == nil && runtime.GOOS == "darwin"
	if err != nil {
		s.InstallHint = err.Error()
	} else if !writableDir(filepath.Dir(bundle)) {
		s.CanInstall = false
		s.InstallHint = "layer8 can't write to " + filepath.Dir(bundle) + "; move layer8.app to /Applications or ~/Applications"
	}
	return s
}

func (u *Updater) set(f func(*Status)) {
	u.mu.Lock()
	f(&u.status)
	u.mu.Unlock()
}

func canonical(v string) string {
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	return semver.Canonical(v)
}

// Newer reports whether latest is a higher semantic version than current.
// Dev builds ("dev" or unparsable) never auto-update.
func Newer(latest, current string) bool {
	l, c := canonical(latest), canonical(current)
	if l == "" || c == "" {
		return false
	}
	return semver.Compare(l, c) > 0
}

func AssetName(version string) string {
	return "layer8-" + strings.TrimPrefix(version, "v") + "-macos-universal.zip"
}

func (u *Updater) client() (*github.Client, error) {
	tok, err := u.token()
	if err != nil {
		return nil, err
	}
	return github.New(tok), nil
}

// Check looks up the latest release.
func (u *Updater) Check(ctx context.Context) (Status, error) {
	u.set(func(s *Status) { s.State, s.Error = "checking", "" })
	fail := func(err error) (Status, error) {
		u.set(func(s *Status) { s.State, s.Error = "error", err.Error() })
		return u.Status(), err
	}
	c, err := u.client()
	if err != nil {
		return fail(err)
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	rel, err := c.LatestRelease(ctx, Owner, Repo)
	if err != nil {
		return fail(err)
	}
	u.mu.Lock()
	u.release = rel
	u.status.Latest = rel.TagName
	u.status.Notes = rel.Body
	u.status.URL = rel.HTMLURL
	u.status.Available = Newer(rel.TagName, u.current)
	u.status.State = "idle"
	u.status.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	u.mu.Unlock()
	return u.Status(), nil
}

// BundlePath returns the .app bundle this process runs from.
func BundlePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(exe); err == nil {
		exe = r
	}
	// <bundle>.app/Contents/MacOS/<binary>
	bundle := filepath.Dir(filepath.Dir(filepath.Dir(exe)))
	if !strings.HasSuffix(bundle, ".app") || filepath.Base(filepath.Dir(exe)) != "MacOS" {
		return "", errors.New("not running from an app bundle (development build)")
	}
	return bundle, nil
}

func writableDir(dir string) bool {
	f, err := os.CreateTemp(dir, ".layer8-write-test-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

// Install downloads, verifies and swaps in the latest release, then calls relaunch.
// relaunch should quit the app; a helper process reopens the new bundle once it has exited.
func (u *Updater) Install(ctx context.Context, relaunch func()) error {
	u.mu.Lock()
	rel := u.release
	available := u.status.Available
	busy := u.status.State == "downloading" || u.status.State == "installing"
	u.mu.Unlock()
	if busy {
		return errors.New("an update is already being installed")
	}
	if rel == nil || !available {
		return errors.New("no update available; check first")
	}
	err := u.install(ctx, rel)
	if err != nil {
		u.set(func(s *Status) { s.State, s.Error = "error", err.Error() })
		return err
	}
	u.set(func(s *Status) { s.State = "ready" })
	relaunch()
	return nil
}

func (u *Updater) install(ctx context.Context, rel *github.Release) error {
	if runtime.GOOS != "darwin" {
		return errors.New("updates are only installed on macOS")
	}
	bundle, err := BundlePath()
	if err != nil {
		return err
	}
	var zipAsset, sumAsset *github.Asset
	for i := range rel.Assets {
		switch rel.Assets[i].Name {
		case AssetName(rel.TagName):
			zipAsset = &rel.Assets[i]
		case checksumsName:
			sumAsset = &rel.Assets[i]
		}
	}
	if zipAsset == nil || sumAsset == nil {
		return fmt.Errorf("release %s is missing %s or %s", rel.TagName, AssetName(rel.TagName), checksumsName)
	}
	c, err := u.client()
	if err != nil {
		return err
	}
	// Stage next to the bundle so the final rename stays on one volume.
	stage, err := os.MkdirTemp(filepath.Dir(bundle), ".layer8-update-")
	if err != nil {
		return fmt.Errorf("can't write next to %s: %w", bundle, err)
	}
	defer os.RemoveAll(stage)

	u.set(func(s *Status) { s.State, s.Error = "downloading", "" })
	dctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()
	var sums strings.Builder
	if err := c.DownloadAsset(dctx, Owner, Repo, sumAsset.ID, &sums); err != nil {
		return fmt.Errorf("download checksums: %w", err)
	}
	want, err := ChecksumFor(sums.String(), zipAsset.Name)
	if err != nil {
		return err
	}
	zipPath := filepath.Join(stage, zipAsset.Name)
	f, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	h := sha256.New()
	err = c.DownloadAsset(dctx, Owner, Repo, zipAsset.ID, io.MultiWriter(f, h))
	f.Close()
	if err != nil {
		return fmt.Errorf("download update: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("checksum mismatch for %s: got %s, want %s", zipAsset.Name, got, want)
	}

	u.set(func(s *Status) { s.State = "installing" })
	out := filepath.Join(stage, "out")
	if b, err := exec.CommandContext(ctx, "/usr/bin/ditto", "-x", "-k", zipPath, out).CombinedOutput(); err != nil {
		return fmt.Errorf("unzip: %v: %s", err, b)
	}
	newBundle := filepath.Join(out, filepath.Base(bundle))
	if _, err := os.Stat(filepath.Join(newBundle, "Contents", "MacOS")); err != nil {
		return fmt.Errorf("the update does not contain %s", filepath.Base(bundle))
	}
	if b, err := exec.CommandContext(ctx, "/usr/bin/codesign", "--verify", "--deep", "--strict", newBundle).CombinedOutput(); err != nil {
		return fmt.Errorf("the update's signature is invalid: %s", strings.TrimSpace(string(b)))
	}
	curTeam, newTeam := TeamID(ctx, bundle), TeamID(ctx, newBundle)
	if curTeam != "" && curTeam != newTeam {
		return fmt.Errorf("the update is signed by team %q, but this app is signed by %q", newTeam, curTeam)
	}
	// Downloads through the API don't get quarantined, but clear it in case.
	_ = exec.CommandContext(ctx, "/usr/bin/xattr", "-dr", "com.apple.quarantine", newBundle).Run()

	old := filepath.Join(stage, "previous.app")
	if err := os.Rename(bundle, old); err != nil {
		return fmt.Errorf("move the current app aside: %w", err)
	}
	if err := os.Rename(newBundle, bundle); err != nil {
		if rerr := os.Rename(old, bundle); rerr != nil {
			return fmt.Errorf("install failed (%v) and restoring the previous app failed too (%v); the previous app is at %s", err, rerr, old)
		}
		return fmt.Errorf("install: %w", err)
	}
	// The stage (with the previous bundle) is removed by the deferred RemoveAll.
	return spawnRelauncher(bundle)
}

// spawnRelauncher reopens bundle after this process has exited.
func spawnRelauncher(bundle string) error {
	script := fmt.Sprintf(`while kill -0 %d 2>/dev/null; do sleep 0.2; done; /usr/bin/open %q`, os.Getpid(), bundle)
	cmd := exec.Command("/bin/sh", "-c", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("schedule relaunch: %w", err)
	}
	go cmd.Process.Release()
	return nil
}

// ChecksumFor finds name in `sha256sum`-style output ("<hex>  <name>").
func ChecksumFor(sums, name string) (string, error) {
	sc := bufio.NewScanner(strings.NewReader(sums))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name && len(f[0]) == 64 {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
}

// TeamID returns the signing Team ID of a bundle, or "" for ad-hoc/unsigned.
func TeamID(ctx context.Context, bundle string) string {
	out, _ := exec.CommandContext(ctx, "/usr/bin/codesign", "-dv", "--verbose=2", bundle).CombinedOutput()
	return ParseTeamID(string(out))
}

func ParseTeamID(codesignOutput string) string {
	for _, line := range strings.Split(codesignOutput, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "TeamIdentifier="); ok {
			if v == "not set" {
				return ""
			}
			return v
		}
	}
	return ""
}
