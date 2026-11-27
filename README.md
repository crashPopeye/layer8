# layer8

A macOS workbench for running many long agent tasks in parallel, built on Claude Code.

- **Tasks start from Linear tickets.** Pick a ticket and layer8 creates a git worktree on Linear's branch name and copies the ticket into the task's docs.
- **Research → plan → implement**, each in a fresh session, handing off through markdown docs. Comment on any line of a doc or a diff and send the comments to the agent in one message.
- **One plan, many PRs.** The plan ends with a `prs:` list. **Split into PRs** creates one stacked branch, one worktree, one Linear sub-issue and one fresh session per PR. When a PR's session finishes, the PRs stacked on it start automatically.
- **Git and GitHub built in.** Each PR has a diff against its parent, a commit list, commit from the UI, push and open a PR, live CI checks and review threads, and **Fix CI** / **Address review** buttons that start a session with the logs or comments.
- **Kept in sync with main.** Every 10 minutes layer8 fetches and rebases each stack onto the default branch. It skips worktrees with running agents or uncommitted changes, never overwrites commits that only exist on GitHub, aborts and flags conflicts (**Resolve conflicts** hands them to an agent), and moves a PR onto `main` when the PR under it merges.
- **Your own terminal and editor**, opened in the right worktree. tmux sessions keep shells alive. **Continue in terminal** runs `claude --resume` on any session.
- **Agents drive the app.** Every session gets two tools, `look` and `act`. `look` shows layer8 as a tree of tasks, PRs and sessions, each with the verbs that apply to it. `act` runs one of them: start a session, fix CI, open a PR, or bring a diff in front of you.
- **Your layout, your panels.** Every view is a panel you can split, resize and drag anywhere. Add your own panels and ⌘K commands with one JavaScript file in `~/.layer8/plugins/`. The layout, theme and accent colour are saved.
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
│  ├─ /api/…  JSON API     /api/events  server-sent events
│  └─ /api/look, /api/act, /api/mcp   the same node tree for ⌘K, the CLI and agents
├─ internal/sessions  one `claude` process per active session, over the CLI's
│                     stream-json control protocol (approvals, interrupts, resume)
├─ internal/core      tasks, PR slices, sync loop, GitHub polling, comments, update proxy
├─ internal/{gitx,github,linear,tmux,launch,diff,plan,prompts}
└─ internal/store     SQLite (pure Go) in ~/.layer8/layer8.db
```

Updates: the shell asks the service for `latest.json`. The service fetches it from the private repository with your GitHub login and proxies the download. Tauri verifies the download against the public key built into the app before installing it. After an update the service restarts onto the new version as soon as no agent is working.

## Agents and the app: look and act

`internal/core/verbs.go` lists everything that can be done to layer8. Each verb names the kind of node it applies to (`app`, `task`, `slice`, `session`), its arguments and when it's available. That one declaration feeds three readers:

| Reader | Reads with | Acts with |
|---|---|---|
| Agent sessions (Claude, Codex) | MCP tool `look` | MCP tool `act` |
| Terminal | `layer8 look [REF]` | `layer8 act REF VERB [key=value…]` |
| Window | ⌘K, built from the focused task, PR and session | ⌘K entry |

```
$ layer8 look task:eng-123
task:eng-123  Fix login redirect  linear=ENG-123  branch=eng-123-fix-login  [run(kind=research|plan|implement, prompt?), chat(prompt), split, sync, open(target=terminal|editor), archive, show(panel?)]
  slice:eng-123/pr1  Backend guard  pr=#412 OPEN  ci=FAILURE  [implement(prompt?), chat(prompt), push, fix-ci, review, open(target=terminal|editor), show(panel?)]
    session:9c41e0aa  Fix CI pr1  kind=fix_ci  status=running  harness=claude  [say(text), stop, show]
$ layer8 act slice:eng-123/pr1 show panel=pr
sent to the window
```

Sessions get the MCP server automatically. `look` is allowed without asking. `act` goes through the normal approval prompt. Approving tool requests is deliberately not a verb.

## Plugins

A plugin is one ES module in `~/.layer8/plugins/`, loaded when the window opens. It gets the same toolkit as the built-in panels and returns panels and ⌘K commands:

```js
// ~/.layer8/plugins/red-prs.js
export default ({ html, act }) => ({
  panels: [{
    id: "red-prs", title: "Red PRs", icon: "wrench",
    Panel: ({ state }) => html`<div class="block">${state.slices.filter((s) => s.ciState === "FAILURE").map((s) => html`<div>${s.key} ${s.title}</div>`)}</div>`,
  }],
  commands: [{ label: "Fix every red PR", run: ({ state }) =>
    Promise.all(state.slices.filter((s) => s.ciState === "FAILURE").map((s) => act(`slice:${s.id}`, "fix-ci"))) }],
});
```

The toolkit has `html`, `h`, the Preact hooks, `Icon`, `api` (`get`/`post`/`put`/`del`), `act(ref, verb, args)` and `look(ref)`. A panel receives the window's focus and state: `state`, `task`, `slice`, `session`, `setFocus`, `showSession`, `run`, `start`, `reveal`.

## Developing

You need Go (see `go.mod`), Rust, and the Tauri CLI (`cargo install tauri-cli --version 2.12.4 --locked`).

```sh
make test       # Go vet + tests, and the UI layout test (needs Node)
make dev        # run layer8 from source and open it in your browser (Ctrl+C stops it)
make daemon     # the service in the foreground; UI at http://127.0.0.1:7878
make app        # a local layer8.app (macOS)
```

## Releasing

```sh
make release VERSION=0.2.0        # or Actions → Release → Run workflow
```

GitHub Actions tests the code, builds the universal app, signs the update, and publishes a GitHub release. The release contains the disk image (`.dmg`), the same app as a `.zip`, the signed update (`.app.tar.gz`), `latest.json` and `checksums.txt`. Installed apps pick it up within a few hours.

### Repository secrets

| Secret | Required | Value |
|---|---|---|
| `TAURI_SIGNING_PRIVATE_KEY` | **yes** | The update signing key. Its public half is `plugins.updater.pubkey` in `src-tauri/tauri.conf.json`. Without it a release fails, because updates must be signed. |
| `MACOS_CERTIFICATE_P12`, `MACOS_CERTIFICATE_PASSWORD`, `MACOS_SIGNING_IDENTITY` | optional | A Developer ID certificate (base64 .p12) and its identity, e.g. `Developer ID Application: Your Name (TEAMID)`. Without them the app is ad-hoc signed. |
| `APPLE_ID`, `APPLE_TEAM_ID`, `APPLE_APP_PASSWORD` | optional | Notarization, together with a Developer ID. |

Keep the update signing key safe: if it is lost, installed apps can't verify new updates and need one manual reinstall with a new key.
