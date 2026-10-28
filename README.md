# layer8

A macOS workbench for running many long agent tasks in parallel, built on Claude Code.

- **Tasks start from Linear tickets.** Pick a ticket and layer8 creates a git worktree on Linear's branch name and copies the ticket into the task's docs.
- **Research → plan → implement**, each in a fresh session, handing off through markdown docs. Comment on any line of a doc or a diff and send the comments to the agent in one message.
- **One plan, many PRs.** The plan ends with a `prs:` list. **Split into PRs** creates one stacked branch, one worktree, one Linear sub-issue and one fresh session per PR. When a PR's session finishes, the PRs stacked on it start automatically.
- **Git and GitHub built in.** Each PR has a diff against its parent, a commit list, commit from the UI, push and open a PR, live CI checks and review threads, and **Fix CI** / **Address review** buttons that start a session with the logs or comments.
- **Kept in sync with main.** Every 10 minutes layer8 fetches and rebases each stack onto the default branch. It skips worktrees with running agents or uncommitted changes, never overwrites commits that only exist on GitHub, aborts and flags conflicts (**Resolve conflicts** hands them to an agent), and moves a PR onto `main` when the PR under it merges.
- **Your own terminal and editor**, opened in the right worktree. tmux sessions keep shells alive. **Continue in terminal** runs `claude --resume` on any session.
- **Agents outlive the window.** A background service runs them; the app and the `layer8` command line are both clients.
- **Signed auto-updates** from the private repository's GitHub Releases.

Install it with [INSTALL.md](INSTALL.md).

## How it works

```
layer8.app
├─ Contents/MacOS/layer8    Tauri v2 shell (Rust, ~150 lines): window, folder picker,
│                           signed updates; starts the service
├─ Contents/MacOS/layer8d   Go: the background service and the `layer8` CLI
└─ ui/ (bundled)            plain HTML, CSS and JS: no Node, no build step

layer8d daemon  (launchd LaunchAgent dev.layer8.daemon, outlives the app)
├─ HTTP on 127.0.0.1:7878, bearer token from ~/.layer8/token, Host/Origin checks
│  └─ /api/…  JSON API     /api/events  server-sent events
├─ internal/sessions  one `claude` process per active session, over the CLI's
│                     stream-json control protocol (approvals, interrupts, resume)
├─ internal/core      tasks, PR slices, sync loop, GitHub polling, comments, update proxy
├─ internal/{gitx,github,linear,tmux,launch,diff,plan,prompts}
└─ internal/store     SQLite (pure Go) in ~/.layer8/layer8.db
```

Updates: the shell asks the service for `latest.json`. The service fetches it from the private repository with your GitHub login and proxies the download. Tauri verifies the download against the public key built into the app before installing it. After an update the service restarts onto the new version as soon as no agent is working.

## Developing

You need Go (see `go.mod`), Rust, and the Tauri CLI (`cargo install tauri-cli --version 2.12.4 --locked`).

```sh
make test       # Go vet + tests (run anywhere)
make daemon     # the service in the foreground; UI at http://127.0.0.1:7878
make app        # a local layer8.app (macOS)
```

## Releasing

```sh
make release VERSION=0.2.0        # or Actions → Release → Run workflow
```

GitHub Actions tests the code, builds the universal app, signs the update, and publishes a GitHub release. The release contains the install zip, the signed update (`.app.tar.gz`), `latest.json` and `checksums.txt`. Installed apps pick it up within a few hours.

### Repository secrets

| Secret | Required | Value |
|---|---|---|
| `TAURI_SIGNING_PRIVATE_KEY` | **yes** | The update signing key. Its public half is `plugins.updater.pubkey` in `src-tauri/tauri.conf.json`. Without it a release fails, because updates must be signed. |
| `MACOS_CERTIFICATE_P12`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY` | optional | A Developer ID certificate (base64 .p12) and its identity, e.g. `Developer ID Application: Your Name (TEAMID)`. Without them the app is ad-hoc signed. |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | optional | Notarization, together with a Developer ID. |

Keep the update signing key safe: if it is lost, installed apps can't verify new updates and need one manual reinstall with a new key.
