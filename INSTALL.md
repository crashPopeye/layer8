# Installing layer8

layer8 is a macOS app (Apple Silicon and Intel, one universal build). It needs `git`, the [GitHub CLI](https://cli.github.com) logged in (`gh auth login`), and [Claude Code](https://claude.com/claude-code) (`claude`) and/or Codex (`codex`) on your login shell's `PATH`.

## Install from the DMG

1. Download `layer8_<version>_universal.dmg` from the [latest release](https://github.com/crashPopeye/layer8/releases/latest). The repository is private: use the browser while logged in, or `gh release download --repo crashPopeye/layer8 --pattern '*.dmg'`.
2. Open the DMG and drag **layer8** into **Applications**.
3. Open layer8. The first launch installs the background daemon (`layer8d`) as the launchd agent `dev.layer8.daemon`, so sessions keep running when the window is closed.

### "layer8 is damaged" or "cannot be opened"

Releases are ad-hoc signed unless they were built with an Apple Developer ID, so Gatekeeper blocks the downloaded copy. Clear the quarantine flag once:

```sh
xattr -dr com.apple.quarantine /Applications/layer8.app
```

Or right-click layer8 in Finder → **Open** → **Open**. Updates installed from inside the app are not quarantined.

## The `layer8` command line

The app bundles the CLI; link it onto your `PATH`:

```sh
sudo ln -sf /Applications/layer8.app/Contents/MacOS/layer8d /usr/local/bin/layer8
layer8 get                     # the app node: projects and workflows
layer8 do app add-project path=$PWD
```

## Updates

layer8 checks for updates on launch and from ⌘K → **Check for updates**. The daemon fetches the latest GitHub release with your `gh` token (the repository is private), the app verifies the update's signature, installs it and restarts. Running sessions are not interrupted: the daemon moves to the new version once no session is busy.

## Files

Everything lives in `~/.layer8` (set `LAYER8_HOME` to move it):

| Path | What |
|---|---|
| `config.json` | settings: `addr`, `maxSessions`, `apps`, `githubToken`, `autoApproveDo`, `updateRepo` |
| `token` | the API token the app, the CLI and agents use (0600) |
| `layer8.db` | projects, tasks, sessions, transcripts, workflow runs, `actions_log` |
| `tasks/<task>/` | each task tree's artifacts (ticket, research, plan, PR description) |
| `worktrees/<project>/<task>/` | one git worktree per task and sub-task |
| `workflows/*.yaml`, `plugins/*.js`, `themes/*.json`, `ui.json` | your workflows, panel plugins, themes and layout |
| `plugin/` | the bundled layer8 Claude Code plugin, reinstalled at every daemon start |
| `logs/daemon.log` | daemon log |

A `layer8.db` left by the pre-rebuild layer8 is moved to `layer8.db.legacy` on first start.

## Uninstall

```sh
launchctl bootout gui/$(id -u)/dev.layer8.daemon
rm ~/Library/LaunchAgents/dev.layer8.daemon.plist /usr/local/bin/layer8
rm -rf /Applications/layer8.app      # and ~/.layer8 to drop all data
```
