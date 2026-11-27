---
title: Panels & customisation
---
# Panels & customisation

The window is a dock of panels that all follow the task you select. You can drag panels into any layout, pop a group out into its own window, switch between saved layouts with ⌘K, restyle the app with themes, and add your own panels as JavaScript plugins.

## The panels

| Panel | Shows | Default place |
|---|---|---|
| **Tasks** | projects, tasks, sub-tasks and their sessions; **Add project**, **New task** | left |
| **Session** | the selected session's transcript, approvals and message input; **Send now** delivers a queued message mid-turn | centre |
| **Artifacts** | the task tree's artifacts, rendered, with comments | right |
| **PR** | the task's pull request: state, checks, review threads | right |
| **Files** | the task's worktree | right |
| **Changes** | the diff against the base branch, line comments, **Commit**, **Push**, **Open PR** | bottom right |
| **Theme** | the theme editor | right |
| **Plugins** | your JS plugins and their status | right |

Open any panel with ⌘1–9 or from ⌘K. The footer shows token usage and rate-limit windows for your agents.

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

## Links and Linear

A task can point at something outside layer8, such as an issue in your tracker. The link shows as a pill in the top bar; an empty `url` clears it.

```sh
layer8 do task:fix-invoice-rounding set-link url=https://linear.app/acme/issue/ENG-482 label=ENG-482
```

Tracker integrations ship as plugins, not in the core. `examples/plugins/linear.js` lists your open Linear issues with your own API key and starts a task from one:

```sh
curl -o ~/.layer8/plugins/linear.js https://raw.githubusercontent.com/crashPopeye/layer8/main/examples/plugins/linear.js
```

[← Workflows & skills](workflows.md) · [Configuration →](configuration.md)
