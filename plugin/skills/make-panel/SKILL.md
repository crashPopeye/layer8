---
name: make-panel
description: Build a custom layer8 panel from a plain-language request ("a panel listing my open PRs across projects") - write it as a JS plugin in ~/.layer8/plugins and open it in the window.
argument-hint: "<what the panel should show>"
---

# Make a panel

A layer8 plugin is one JavaScript file in `~/.layer8/plugins/` (or `$LAYER8_HOME/plugins/`). The app hot-reloads it, and its panels join the dock like built-in ones.

## 1. Learn the SDK

Read `layer8://ui/plugins` (MCP server `layer8`). When it documents the plugin SDK or lists existing plugins, follow it over the sketch below; reading an existing plugin is the fastest way to match the API.

## 2. Find the data

Panels read the same tree you do. Read the nodes the panel needs (`layer8://app` for projects and workflows, `layer8://project/<name>` for tasks, `layer8://task/<path>` for branches, PRs, sessions) and note the fields to show and the actions to offer as buttons.

## 3. Write the plugin

File `~/.layer8/plugins/<panel-id>.js`, where `<panel-id>` is kebab case. Sketch:

```js
export default function setup(sdk) {
  const { React, ui } = sdk;

  function OpenPRs() {
    const app = sdk.useNode("app", 2);
    const tasks = (app?.children ?? []).flatMap((p) => p.children ?? []).filter((t) => t.kind === "task");
    return React.createElement(ui.ScrollArea, { className: "h-full p-2" },
      tasks.map((t) => React.createElement(ui.Button, {
        key: t.addr, variant: "ghost", onClick: () => sdk.focus(t.addr),
      }, t.title)));
  }

  sdk.registerPanel({ id: "open-prs", title: "Open PRs", icon: "GitPullRequest", component: OpenPRs, defaultPosition: "right" });
}
```

Rules: no build step and no imports (everything comes from `sdk`); read with `sdk.get` / `sdk.watch` / `sdk.useNode`, act with `sdk.do(ops)`; use the shadcn components in `sdk.ui` and theme classes, never hard-coded colours; keep it under ~150 lines.

## 4. Open it

```json
{"ops":[{"target":"ui:layout","action":"open-panel","args":{"panel":"open-prs"}}]}
```

If the result is an error, read `layer8://ui/layout` and `layer8://ui/plugins` for the plugin's load error, fix the file, and retry. If `ui:layout` doesn't exist yet in this version of layer8, tell the user the file path and that the panel appears once plugins are supported.

## 5. Finish

Tell the user the file path and how to change the panel later (edit the file, or ask again). Then, if `advance` is listed on `here`:

```json
{"ops":[{"target":"here","action":"advance"}]}
```
