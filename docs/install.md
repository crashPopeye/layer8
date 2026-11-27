---
title: Install
---
# Install

Download the DMG from the [latest release](https://github.com/crashPopeye/layer8/releases/latest), drag layer8 into Applications, and clear the quarantine flag once. layer8 runs on macOS (one universal build for Apple Silicon and Intel) and needs `git` plus at least one agent CLI. A GitHub login is only needed to open pull requests.

## Prerequisites

| Tool | Needed for | Check |
|---|---|---|
| macOS | the app | — |
| `git` | projects, worktrees, commits | `git --version` |
| [Claude Code](https://claude.com/claude-code) (`claude`) and/or Codex (`codex`) | agent sessions | `claude --version` / `codex --version` |
| [GitHub CLI](https://cli.github.com) (`gh`), logged in | opening PRs, PR status, CI logs. Optional | `gh auth status` |

The daemon finds `claude`, `codex` and `gh` on your **login shell's `PATH`** (it reads `$SHELL -ilc env`), so whatever works in a new Terminal window works in layer8.

## Install from the DMG

1. Download `layer8_<version>_universal.dmg` (for example `layer8_1.0.0_universal.dmg`) from the [latest release](https://github.com/crashPopeye/layer8/releases/latest).
2. Open the DMG and drag **layer8** into **Applications**.
3. Open layer8. The first launch installs the background daemon `layer8d` as the launchd agent `dev.layer8.daemon`, listening on `127.0.0.1:7979`, so sessions keep running when the window is closed.

### "layer8 is damaged" or "cannot be opened"

Releases are ad-hoc signed, not notarised by Apple, so Gatekeeper blocks the downloaded copy. Clear the quarantine flag once:

```sh
xattr -dr com.apple.quarantine /Applications/layer8.app
```

Or right-click layer8 in Finder → **Open** → **Open**. Updates installed from inside the app are not quarantined.

## The `layer8` command line

The app bundles the CLI. Link it onto your `PATH`:

```sh
sudo ln -sf /Applications/layer8.app/Contents/MacOS/layer8d /usr/local/bin/layer8
layer8 get
```

`layer8 get` prints the app node: your projects and the available workflows. See [CLI](cli.md).

## What needs GitHub

layer8 works without any GitHub account. A token (from `gh auth login`, or `githubToken` in [`config.json`](configuration.md)) is only used for the GitHub API:

| Feature | Needs `gh auth login` (or `githubToken`) | Why |
|---|---|---|
| Projects, tasks, sessions, workflows, artifacts, comments | no | local only |
| Commit | no | local git |
| Push | no, uses your own git credentials (SSH key / credential helper) | `git push` |
| Open PR, PR panel status, checks, review threads | **yes** | GitHub REST + GraphQL |
| `fix-ci` reading failing job logs | **yes** | Actions API |
| In-app updates | no, a token is used if present | public releases |

When you need it:

```sh
gh auth login
```

## Updates

layer8 checks for updates on launch and from ⌘K → **Check for updates**. The daemon reads the latest GitHub release, the app verifies the update's signature, installs it and restarts. No GitHub login is needed. If a token is available it's sent along, which raises GitHub's limit from 60 to 5,000 requests an hour; if an update check fails with a `403` on a shared network, run `gh auth login`.

Running sessions are not interrupted: the daemon moves to the new version once no session is busy.

## Files

Everything lives in `~/.layer8` (set `LAYER8_HOME` to move it). [Configuration](configuration.md#the-layer8-folder) lists every file.

| Path | What |
|---|---|
| `config.json` | your settings |
| `token` | the API token the app, the CLI and agents use (0600) |
| `layer8.db` | projects, tasks, sessions, transcripts, workflow runs |
| `tasks/<task>/` | each task tree's shared artifacts |
| `worktrees/<project>/<task>/` | one git worktree per task and sub-task |
| `logs/daemon.log` | the daemon's log |

## Uninstall

```sh
launchctl bootout gui/$(id -u)/dev.layer8.daemon
rm ~/Library/LaunchAgents/dev.layer8.daemon.plist /usr/local/bin/layer8
rm -rf /Applications/layer8.app
```

To drop all data too, `rm -rf ~/.layer8`, then run `git worktree prune` in each project's repository. Task branches stay in your repositories.

[← Overview](index.md) · [Getting started →](getting-started.md)
