# layer8

A single-player, local replacement for the HumanLayer desktop app, written in Go.

Projects are git repositories on your Mac. Each task gets its own worktree on its own branch, plus an artifacts folder linked into the worktree at `.layer8/task/`. Every session of the task shares that folder. Everything lives under `~/.layer8/`; nothing goes to a cloud.

```text
~/.layer8
├── layer8.db              projects, tasks, sessions and their conversations (SQLite)
├── token                  API token for the UI and the l8 CLI (0600)
├── plugin/<version>/      the l8 Claude Code plugin every session loads
├── logs/                  daemon.log, claude-<sessionId>.stderr
├── artifacts/<taskId>/    task.md and every artifact of the task
└── workspaces/<slug>/     the task's git worktree, branch l8/<slug>
    └── .layer8/task  →  ~/.layer8/artifacts/<taskId>   (git-excluded)
```

## Pieces

| Binary | What it does |
|---|---|
| `layer8` | The window: shows the UI signed in, a menu-bar badge counting sessions waiting for approval, and notifications. Closing it only hides it. |
| `layer8 daemon` | Serves the JSON API, the SSE stream and the UI on `127.0.0.1:7878` (loopback only, token or cookie auth). The LaunchAgent `dev.layer8.daemon` keeps it running. |
| `l8` | A thin CLI client of the daemon, for you and for agents. |
| `claude` | One child of the daemon per live session, over stream-json, with your full Claude Code config plus the `l8` plugin. |

## Install

```sh
make app     # build layer8.app (ad-hoc signed) and copy it to /Applications
```

Opening the app runs `layer8 daemon ensure`: it writes `~/Library/LaunchAgents/dev.layer8.daemon.plist` for the app's binary, replaces whatever daemon ran under that label (the old layer8 included) and waits for the new one. A database left by the old layer8 is moved to `~/.layer8/legacy-<date>.db` on first start.

Sessions survive a daemon restart: a session that was mid-turn resumes its transcript with `--resume` and "Continue where you left off.", its pending approvals go stale and are asked again; idle sessions resume on their next message.

`make app APP_DEST=` builds `build/out/layer8.app` without installing it.

## Develop

```sh
make check   # gofmt, go vet, go test -race: the only CI
make dev     # build, run the daemon, open the UI signed in
```

`make dev` keeps its state in `~/.layer8-dev` and listens on `127.0.0.1:7879`, so it runs next to an installed build. `build/out/layer8` opens the window on the dev daemon; a dev build never installs the LaunchAgent. Override with `LAYER8_HOME=$(mktemp -d)` or `LAYER8_ADDR=127.0.0.1:7900`. The CLI reads the same two variables:

```sh
export LAYER8_HOME=~/.layer8-dev LAYER8_ADDR=127.0.0.1:7879
build/out/l8 project add ~/Work/layer8
build/out/l8 task create --title "Smoke task" --prompt "Try it out"
build/out/l8 task list
build/out/l8 task show smoke-task
build/out/l8 session start --task smoke-task --label research --prompt "Run go version"
build/out/l8 session list --task smoke-task
build/out/l8 session read <session-id> --last 5
```

GitHub Actions run only on manual dispatch, so pushing a branch costs nothing.
