# Installing layer8

layer8 runs on macOS 12 or later, on both Apple Silicon and Intel Macs. Setup takes about five minutes.

## 1. Install the tools layer8 uses

Open Terminal and check each one. If a command is missing, install it with the line next to it.

| Check | If it's missing |
|---|---|
| `claude --version` | Install Claude Code from https://claude.com/claude-code, then run `claude` once and log in |
| `git --version` | `xcode-select --install` |
| `gh --version` | `brew install gh` |
| `tmux -V` (optional, recommended) | `brew install tmux` |

Then log in to GitHub, once:

```sh
gh auth login
```

layer8 uses this login to download itself and its updates (the repository is private), and to open and track your pull requests.

## 2. Install the app

**With the disk image:** download `layer8-<version>-macos-universal.dmg` from the [latest release](https://github.com/crashPopeye/layer8/releases/latest), open it and drag **layer8** onto **Applications**. Because the app isn't notarized by Apple yet, macOS blocks the first launch. Run this once, then open layer8 again:

```sh
xattr -dr com.apple.quarantine /Applications/layer8.app
```

**Or from Terminal:** these three lines download and install it without the quarantine step:

```sh
gh release download --repo crashPopeye/layer8 --pattern 'layer8-*-macos-universal.zip' --dir /tmp/layer8 --clobber
ditto -x -k /tmp/layer8/layer8-*-macos-universal.zip /Applications
open /Applications/layer8.app
```

That's it: layer8 opens.

The first time it opens, macOS shows a **"Background Items Added"** notice for layer8. That's expected: layer8 runs a small background service, so your agents keep working after you close the window or quit the app. You can see it in **System Settings → General → Login Items & Extensions**.

## 3. First launch

1. Press **,** (comma) to open Settings.
2. **Workspaces:** click **Choose folder…** and pick a project folder. Any folder works. Git repositories also get a separate worktree for each task and each PR.
3. **Linear API key:** in Linear, open **Settings → Security & access → Personal API keys**, create a key and paste it here.
4. **Terminal** and **Editor:** pick the apps you use.
5. **Command line:** click **Install the layer8 command**, then try `layer8 help` in a terminal.
6. Click **Save**.

Press **N** to start a task from one of your Linear tickets.

The first time you click **Terminal**, macOS asks whether layer8 may control Terminal (or iTerm). Click **OK**: this is how layer8 opens a window in the task's folder.

## Updates

layer8 checks for new versions every few hours. When one is available, a bar appears at the top of the window: click **Install and restart**. Updates are signed, and layer8 refuses any download that doesn't match its signing key.

Agents that are working when you update keep running. The background service switches to the new version by itself once they finish.

## The command line

```sh
layer8 status                       # agents working, approvals waiting
layer8 tasks                        # tasks, their PRs and sessions
layer8 new ENG-123                  # start a task from a Linear ticket
layer8 run ENG-123 plan             # start a workflow
layer8 log <session> -f             # follow an agent live
layer8 approve <id>                 # answer a permission prompt
layer8 help                         # everything else
```

## Where things live

| What | Where |
|---|---|
| Settings, database, API token | `~/.layer8/` |
| Research, plans and handoffs for each task | `~/.layer8/tasks/<task>/` |
| Worktrees | `~/.layer8/worktrees/<repo>/<task>` (you can change this in Settings) |
| Logs | `~/.layer8/logs/layer8d.log` |

## Troubleshooting

- **"layer8 can't start its service":** look at `~/.layer8/logs/layer8d.log`. If port 7878 is taken by another program, set `"port"` in `~/.layer8/config.json` and reopen layer8.
- **"claude CLI not found":** layer8 reads your login shell's `PATH`. Check that `claude --version` works in a new Terminal window, then quit layer8 (⌘Q) and reopen it.
- **"no GitHub token":** run `gh auth login`, or paste a token in **Settings → GitHub token**.
- **The window disappeared:** closing the window only hides it. Click the layer8 icon in the Dock to bring it back.

## Uninstall

```sh
/Applications/layer8.app/Contents/MacOS/layer8d uninstall-agent   # stops the background service
rm -rf /Applications/layer8.app ~/.layer8
rm -f /usr/local/bin/layer8 ~/.local/bin/layer8
```

Your repositories and branches are not touched. Worktrees under `~/.layer8/worktrees` are removed with it, so commit or push anything you want to keep first.
