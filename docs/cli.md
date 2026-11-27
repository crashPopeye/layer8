---
title: CLI
---
# CLI

The `layer8` command reads and acts on the same node tree as the window, through the running daemon. It has two verbs: `layer8 get <address>` prints a node with its children and the actions valid on it now, and `layer8 do <address> <action> key=value…` applies one. Everything the window does, you can script.

## Set it up

The CLI ships inside the app. Link it onto your `PATH` once:

```sh
sudo ln -sf /Applications/layer8.app/Contents/MacOS/layer8d /usr/local/bin/layer8
```

It talks to the daemon at `addr` from [`config.json`](configuration.md) (default `127.0.0.1:7979`) with the token in `~/.layer8/token`. If the daemon isn't running, open the app or run `layer8 ensure`.

## Usage

| Command | Does |
|---|---|
| `layer8 get [addr] [--json] [--depth 0\|1\|2]` | read a node; `addr` defaults to `app`, depth to `1` |
| `layer8 do <addr> <action> [k=v\|k:=json]...` | apply an action. `k=v` passes a string, `k:=json` passes a number, boolean, array or object |
| `layer8 do --json '[{"target","action","args"}…]'` | apply several actions in one call |
| `layer8 serve` | run the daemon in the foreground (launchd does this for you) |
| `layer8 ensure` | install or refresh the launchd agent and print `{"url","token"}` |

`layer8 do` exits non-zero when any action fails.

## Examples

Read the app, a project, a task:

```sh
layer8 get
layer8 get project:billing-api
layer8 get task:fix-invoice-rounding --json
```

The output ends with the node's actions and their arguments, so `layer8 get` is also the help for `layer8 do`.

The [Getting started](getting-started.md) walkthrough, from the terminal:

```sh
cd ~/code/billing-api
layer8 do app add-project path=$PWD
layer8 do project:billing-api create-task title="Fix invoice rounding"
layer8 do task:fix-invoice-rounding run-workflow workflow=quick-fix
layer8 do task:fix-invoice-rounding next-step
```

Start a session with a prompt, then talk to it:

```sh
layer8 do task:fix-invoice-rounding start-session prompt="Round invoice totals half-even" harness=codex
layer8 do session:3f9a2c1d say text="Also add a test for 0.005"
layer8 do session:3f9a2c1d allow
```

Split a task into stacked sub-tasks and run a workflow on autopilot:

```sh
layer8 do task:eng-482 create-subtask name=pr1-schema
layer8 do task:eng-482 create-subtask name=pr2-api base=pr1-schema
layer8 do task:eng-482 run-workflow workflow=feature autopilot:=true
```

Several actions at once:

```sh
layer8 do --json '[{"target":"task:fix-invoice-rounding","action":"commit","args":{"message":"Round half-even"}},
                   {"target":"task:fix-invoice-rounding","action":"open-pr","args":{"draft":true}}]'
```

## Other doors

The CLI is one of four ways into the node tree; they all take the same actions.

| Door | Read | Act |
|---|---|---|
| CLI | `layer8 get project:billing-api` | `layer8 do app add-project path=…` |
| HTTP | `GET /api/node?addr=app&depth=1` | `POST /api/do` `{"ops":[{"target","action","args"}]}` |
| MCP (`POST /mcp`, for agents) | resource `layer8://project/billing-api`, or `layer8://here` | tool `do` |
| Live events | `GET /api/events` (SSE) | — |

Every HTTP call needs `Authorization: Bearer $(cat ~/.layer8/token)`:

```sh
curl -s -H "Authorization: Bearer $(cat ~/.layer8/token)" "http://127.0.0.1:7979/api/node?addr=app&depth=1"
```

[← Configuration](configuration.md) · [Contributing →](contributing.md)
