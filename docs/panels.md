---
title: Panels & customisation
---
# Panels & customisation

The window is a dock of panels that all follow the task you select. You can drag panels into any layout, pop a group out into its own window, switch between saved layouts with ⌘K, restyle the app with themes, and add your own panels as JavaScript plugins. Linear issues have their own panel.

## The panels

| Panel | Shows | Default place |
|---|---|---|
| **Tasks** | projects, tasks, sub-tasks and their sessions; **Add project**, **New task** | left |
| **Linear** | your open Linear issues; create a task from one | left |
| **Session** | the selected session's transcript, approvals and message input | centre |
| **Artifacts** | the task tree's artifacts, rendered, with comments | right |
| **PR** | the task's pull request: state, checks, review threads | right |
| **Files** | the task's worktree | right |
| **Changes** | the diff against the base branch, line comments, **Commit**, **Push**, **Open PR** | bottom right |
| **Theme** | the theme editor | right |
| **Plugins** | your JS plugins and their status | right |

Open any panel with ⌘1–9 or from ⌘K.

## Keys

| Key | Does |
|---|---|
| ⌘K | command palette: panels, presets, actions, **Check for updates** |
| ⌘N | new task |
| ⌘⇧N | new sub-task of the selected task |
| ⌘T | new session in the selected task |
| ⌘1–9 | open panel 1–9 |
| A / D | allow / deny the pending tool call |

## Layouts and presets

Drag a panel's tab to split or stack it, or pop a group out into its own window. The layout is saved to `~/.layer8/ui.json` as it changes.

⌘K switches between presets: **Default**, **Focus**, **Review** and **Wide diff**, plus any you save. From the CLI:

```sh
layer8 do ui:layout preset name=Review
layer8 do ui:layout save-preset name=Triage
```

## Themes

The **Theme** panel edits the colour tokens with a live preview, switches dark, light or system, and sets an accent colour. A theme is a JSON file of token overrides saved in `~/.layer8/themes/`:

```json
{"shared": {"primary": "#38bdf8"}, "dark": {"background": "#0a0f16", "foreground": "#e6edf5"}, "light": {"background": "#f8fbff"}}
```

## Plugins

A plugin is an ES module in `~/.layer8/plugins/<name>.js`. It loads into the window and reloads when you save it, and it can register panels and ⌘K commands. `layer8 get ui:plugins` prints the plugin SDK and each plugin's status.

`examples/plugins/open-prs.js` in the repository is a complete example: a panel listing the open PRs across all tasks. Copy it to try it:

```sh
curl -o ~/.layer8/plugins/open-prs.js https://raw.githubusercontent.com/crashPopeye/layer8/main/examples/plugins/open-prs.js
```

Or ask an agent: the `make-panel` skill writes the plugin from a plain-language request.

## Linear

Connect Linear once, then link tasks to issues. Credentials live in `~/.layer8/linear.json` (0600).

- **API key (default):** create one in Linear → Settings → Security & access → Personal API keys, then `layer8 do linear connect apiKey=lin_api_…`.
- **Sign in with Linear (optional):** create your own OAuth app in Linear → Settings → API → OAuth applications with the callback URL `http://127.0.0.1:7979/api/linear/callback`, put its client id in `~/.layer8/config.json` as `linearClientId`, restart the daemon, then run `layer8 do linear connect-oauth` and open the returned URL. PKCE is used, so no client secret is stored.

The `linear` node lists your open issues as `linear:<ID>` children; `layer8 get linear:ENG-482` reads one, and `layer8 do linear search query=upload` searches.

Tasks link to issues, they don't mirror them. A linked task's state is re-read from Linear at most once a minute.

| Action | On | Does |
|---|---|---|
| `create-task-from-linear issue=ENG-482` | project | a task titled like the issue, on Linear's branch name, with `01-ticket.md` in the artifacts, linked |
| `link-linear issue=…` / `refresh-linear` / `unlink-linear` | task | set, re-read or clear the link; root tasks (re)write `01-ticket.md` |
| `create-subtask name=… linear:=true` | task | also creates a Linear sub-issue under the task's issue and links it |
| `open-pr` | linked task | also attaches the pull request to the issue |

[← Workflows & skills](workflows.md) · [Configuration →](configuration.md)
