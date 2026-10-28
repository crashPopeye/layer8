# layer8

A macOS workbench for running many long agent tasks in parallel, built on Claude Code.

- **Tasks start from Linear tickets.** Pick a ticket and layer8 creates a git worktree on Linear's branch name and copies the ticket into the task's docs.
- **Research → plan → implement**, each in a fresh session, handing off through markdown docs. Comment on any line of a doc or a diff and send the comments to the agent in one message.
- **One plan, many PRs.** The plan ends with a `prs:` list. **Split into PRs** creates one stacked branch, one worktree, one Linear sub-issue and one fresh session per PR. When a PR's session finishes, the PRs stacked on it start automatically.
- **Git and GitHub built in.** Each PR has a diff against its parent, a commit list, commit from the UI, push and open a PR, live CI checks and review threads, and **Fix CI** / **Address review** buttons that start a session with the logs or comments.
- **Kept in sync with main.** Every 10 minutes layer8 fetches and rebases each stack onto the default branch. It skips worktrees with running agents or uncommitted changes, aborts and flags conflicts (**Resolve conflicts** hands them to an agent), and moves a PR onto `main` when the PR under it merges.
- **Your own terminal and editor**, opened in the right worktree. tmux sessions keep shells alive between windows. **Continue in terminal** runs `claude --resume` on any session.
- **Self-updating** from GitHub Releases.

Install it with [INSTALL.md](INSTALL.md).

## How it works

```
layer8.app (one Go binary)
├─ Wails v3 window ── ui/ (plain HTML, CSS and JS: no Node, no build step)
│                      └─ fetch('/api/…') ──► internal/core (JSON API, same origin)
├─ internal/sessions ─ one `claude` process per active session, over the CLI's
│                      stream-json control protocol (approvals, interrupts, resume)
├─ internal/core ───── tasks, PR slices, sync loop, GitHub polling, comments
├─ internal/{gitx,github,linear,tmux,launch,diff,plan,prompts}
├─ internal/store ──── SQLite (pure Go) in ~/.layer8/layer8.db
└─ internal/updater ── GitHub Releases, SHA-256 check, codesign check, bundle swap
```

The only non-Go code is the frontend in `ui/`: about 1,500 lines of plain ES modules, plus `ui/vendor/preact-htm.js` (13 KB, vendored, no tooling).

## Developing

You need Go 1.26 (`go.mod` says which version). The app itself builds only on macOS.

```sh
make test                         # vet + tests (pure Go; runs on Linux too)
make dev                          # UI + API in your browser at http://127.0.0.1:7999
make app                          # build/out/layer8.app
```

On Linux the desktop shell also compiles with `go build -tags gtk3 .`, which is useful as a sanity check.

## Releasing

```sh
make release VERSION=0.2.0        # tags v0.2.0 and pushes it
```

Or go to the **Actions** tab → **Release** → **Run workflow** and enter the version. GitHub Actions tests the code, builds the universal app, signs it, and publishes `layer8-<version>-macos-universal.zip` and `checksums.txt` as a GitHub release. Running apps pick it up within a few hours.

### Signing with an Apple Developer ID (optional)

Without these secrets, releases are ad-hoc signed. They run fine, but a copy downloaded through a browser has to be allowed once (see INSTALL.md). To sign and notarize, add these repository secrets (Settings → Secrets and variables → Actions):

| Secret | Value |
|---|---|
| `MACOS_CERTIFICATE_P12` | Your "Developer ID Application" certificate exported as .p12, base64-encoded (`base64 -i cert.p12 \| pbcopy`) |
| `MACOS_CERTIFICATE_PASSWORD` | The .p12 export password |
| `MACOS_SIGNING_IDENTITY` | e.g. `Developer ID Application: Your Name (TEAMID)` |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | For notarization; the password is an app-specific password from appleid.apple.com |

Once releases are signed with a Team ID, the updater only accepts updates signed by that same team.
