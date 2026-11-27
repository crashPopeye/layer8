---
title: Configuration
---
# Configuration

layer8 reads its settings from `~/.layer8/config.json` when the daemon starts. The file is optional: every key has a default, and you only write the ones you change. Restart the daemon after editing it. The daemon outlives the window, so restart it with launchd:

```sh
launchctl kickstart -k gui/$(id -u)/dev.layer8.daemon
```

```json
{
  "maxSessions": 4,
  "apps": {"terminal": "iTerm", "editor": "Cursor"},
  "autoApproveDo": true
}
```

## Settings

| Key | Default | What it does |
|---|---|---|
| `addr` | `"127.0.0.1:7979"` | where the daemon listens. The app, the CLI and agents all connect here |
| `maxSessions` | `6` | how many agent sessions run at once; more are queued |
| `apps.terminal` | `"Terminal"` | the macOS app a task opens in (`open target=terminal`), and where a session continues with `open-terminal` |
| `apps.editor` | `"Visual Studio Code"` | the macOS app a task opens in with `open target=editor` |
| `linearClientId` | `""` | client id of your own Linear OAuth app, for **Sign in with Linear**. See [Linear](panels.md#linear) |
| `githubToken` | `""` | GitHub token for PRs, checks and CI logs. Empty uses `gh auth token`. See [What needs GitHub](install.md#what-needs-github) |
| `autoApproveDo` | `false` | let sessions call the layer8 `do` tool without asking you. See [approvals](concepts.md#approvals) |
| `updateRepo` | `"crashPopeye/layer8"` | the GitHub repository whose latest release the app updates from |

### Advanced

| Key | Default | What it does |
|---|---|---|
| `pluginDir` | `""` | the layer8 Claude Code plugin (skills) sessions load. Empty installs the bundled one into `~/.layer8/plugin` at every start. Point it at a checkout of `plugin/` to work on the skills |
| `githubApi` | `"https://api.github.com"` | base URL of the GitHub REST and GraphQL APIs, for GitHub Enterprise or tests |

## The `~/.layer8` folder

Set `LAYER8_HOME` to use another folder (the daemon's development mode uses `./.dev-home`).

| Path | What |
|---|---|
| `config.json` | the settings above |
| `token` | the API token the app, the CLI and agents use (0600), created on first start |
| `layer8.db` | SQLite: projects, tasks, sessions, transcripts, workflow runs, comments, and `actions_log` (every action and who did it) |
| `tasks/<task>/` | each task tree's shared artifacts (ticket, research, plan, PR description) |
| `worktrees/<project>/<task>/` | one git worktree per task and sub-task |
| `workflows/*.yaml` | your workflows. See [Workflows & skills](workflows.md#write-your-own-workflow) |
| `plugins/*.js` | your panel plugins |
| `themes/*.json` | your themes |
| `ui.json` | window layout and saved presets |
| `plugin/` | the bundled layer8 skills, reinstalled at every daemon start |
| `linear.json` | Linear credentials (0600) |
| `logs/daemon.log` | the daemon's log |

A `layer8.db` left by a pre-1.0 layer8 is moved to `layer8.db.legacy` on first start.

[← Panels & customisation](panels.md) · [CLI →](cli.md)
