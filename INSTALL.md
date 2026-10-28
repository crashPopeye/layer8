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

Copy and paste these three lines into Terminal:

```sh
gh release download --repo crashPopeye/layer8 --pattern 'layer8-*-macos-universal.zip' --dir /tmp/layer8 --clobber
ditto -x -k /tmp/layer8/layer8-*-macos-universal.zip /Applications
open /Applications/layer8.app
```

That's it: layer8 opens.

> **If you downloaded the zip from the GitHub website instead,** macOS will say it "cannot verify" layer8, because the app isn't notarized by Apple yet. Run this once and open it again:
> ```sh
> xattr -dr com.apple.quarantine /Applications/layer8.app
> ```

## 3. First launch

1. Press **,** (comma) to open Settings.
2. **Workspaces:** click **Choose folder…** and pick a project folder. Any folder works. Git repositories also get a separate worktree for each task and each PR.
3. **Linear API key:** in Linear, open **Settings → Security & access → Personal API keys**, create a key and paste it here.
4. **Terminal** and **Editor:** pick the apps you use.
5. Click **Save**.

Press **N** to start a task from one of your Linear tickets.

The first time you click **Terminal**, macOS asks whether layer8 may control Terminal (or iTerm). Click **OK**: this is how layer8 opens a window in the task's folder.

## Updates

layer8 checks for new versions every few hours. When one is available, a bar appears at the top of the window: click **Install and restart**. Wait for running agents to finish first; layer8 won't update while they work.

You can also check by hand: **Settings → Updates → Check now**.

## Where things live

| What | Where |
|---|---|
| Settings, database, logs | `~/.layer8/` |
| Research, plans and handoffs for each task | `~/.layer8/tasks/<task>/` |
| Worktrees | `~/.layer8/worktrees/<repo>/<task>` (you can change this in Settings) |
| Log file | `~/.layer8/logs/layer8.log` |

## Troubleshooting

- **"claude CLI not found":** layer8 reads your login shell's `PATH`. Check that `claude --version` works in a new Terminal window, then quit layer8 (⌘Q) and reopen it.
- **"no GitHub token":** run `gh auth login`, or paste a token in **Settings → GitHub token**.
- **The window disappeared:** closing the window only hides it, so agents keep working. Click the layer8 icon in the Dock to bring it back. **⌘Q** quits; the sessions it stops can be resumed later.
- **Anything else:** look at `~/.layer8/logs/layer8.log`.

## Uninstall

```sh
rm -rf /Applications/layer8.app ~/.layer8
```

Your repositories and branches are not touched. Worktrees under `~/.layer8/worktrees` are removed with it, so commit or push anything you want to keep first.
