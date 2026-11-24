# layer8

A single-player, local replacement for the HumanLayer desktop app, written in Go.

Projects are git repositories on your Mac. Each task gets its own worktree on its own branch, plus an artifacts folder linked into the worktree at `.layer8/task/`. Every session of the task shares that folder. Everything lives under `~/.layer8/`; nothing goes to a cloud.

```text
~/.layer8
├── layer8.db              projects, tasks, sessions and their conversations (SQLite)
├── token                  API token for the UI and the l8 CLI (0600)
├── plugin/<version>/      the l8 Claude Code plugin every session loads
├── logs/                  claude-<sessionId>.stderr
├── artifacts/<taskId>/    task.md and every artifact of the task
└── workspaces/<slug>/     the task's git worktree, branch l8/<slug>
    └── .layer8/task  →  ~/.layer8/artifacts/<taskId>   (git-excluded)
```

## Pieces

| Binary | What it does |
|---|---|
| `layer8 daemon` | Serves the JSON API, the SSE stream and the UI on `127.0.0.1:7878` (loopback only, token or cookie auth). |
| `l8` | A thin CLI client of the daemon, for you and for agents. |
| `claude` | One child of the daemon per live session, over stream-json, with your full Claude Code config plus the `l8` plugin. |

## Develop

```sh
make check   # gofmt, go vet, go test -race: the only CI
make dev     # build, run the daemon, open the UI signed in
```

`make dev` keeps its state in `~/.layer8-dev` and listens on `127.0.0.1:7879`, so it runs next to an installed build. Override with `LAYER8_HOME=$(mktemp -d)` or `LAYER8_ADDR=127.0.0.1:7900`. The CLI reads the same two variables:

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
