# layer8

A macOS desktop app for running coding agents on real work. Add a git repository, create a task, and layer8 gives it its own branch and worktree. Claude Code or Codex sessions run inside the task, share one artifacts folder across the whole task tree, and workflows chain skills (research → plan → implement → open the PR) that drive the app themselves. Big work splits into stacked sub-tasks, one pull request each, and a session is two clicks from a reviewed diff and an open PR.

![layer8 main window](docs/screenshot.png)

**Docs:** [crashpopeye.github.io/layer8](https://crashpopeye.github.io/layer8/)

## Install

Needs macOS, `git`, and `claude` and/or `codex` on your login shell's `PATH`.

1. Download `layer8_<version>_universal.dmg` from the [latest release](https://github.com/crashPopeye/layer8/releases/latest).
2. Drag **layer8** into **Applications**.
3. Clear the quarantine flag once, then open it: `xattr -dr com.apple.quarantine /Applications/layer8.app`

A GitHub login (`gh auth login`) is only needed to open pull requests and read CI. Details in [Install](docs/install.md).

## Documentation

| Page | Answers |
|---|---|
| [Overview](docs/index.md) | What layer8 is, who it's for, a day with it |
| [Install](docs/install.md) | Prerequisites, Gatekeeper, the CLI, what needs GitHub, updates, uninstall |
| [Getting started](docs/getting-started.md) | First project → task → workflow → diff → pull request |
| [Concepts](docs/concepts.md) | The node tree, tasks and sub-tasks, worktrees, artifacts, sessions, approvals, comments |
| [Workflows & skills](docs/workflows.md) | The built-in workflows and skills, autopilot, writing your own workflow |
| [Panels & customisation](docs/panels.md) | Panels, layouts, themes, plugins, ⌘K, Linear |
| [Configuration](docs/configuration.md) | Every setting with its default, the `~/.layer8` layout |
| [CLI](docs/cli.md) | `layer8 get` and `layer8 do` |
| [Contributing](docs/contributing.md) | Architecture, repo layout, building from source, releasing |

## License

[Apache-2.0](LICENSE)
