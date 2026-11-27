# layer8 plugin SDK

A plugin is one ES module, `<dir>/<name>.js` (`dir` is a field of `ui:plugins`; names are letters, digits, `-` and `_`). No build step and no imports: everything comes from the `sdk` argument. The window loads every plugin at start and reloads one as soon as its file changes.

```js
export default function setup(sdk) {
  const { React, ui } = sdk;
  const h = React.createElement;

  function Hello() {
    const app = sdk.useNode("app", 2);
    const projects = (app?.children ?? []).filter((c) => c.kind === "project");
    return h(ui.ScrollArea, { className: "h-full p-2" },
      projects.map((p) => h(ui.Button, { key: p.addr, variant: "ghost", onClick: () => sdk.focus(p.addr) }, p.title)));
  }

  sdk.registerPanel({ id: "hello", title: "Hello", icon: "Hand", component: Hello, defaultPosition: "right" });
  sdk.registerCommand({ id: "hello.open", title: "Open the Hello panel", run: () => sdk.openPanel("hello") });
}
```

## sdk

| Member | What it is |
|---|---|
| `name` | The plugin's name |
| `React` | The window's React 19. Use `React.createElement` (JSX needs a build step) and hooks such as `React.useState` |
| `ui` | shadcn components: `Button` (`variant`: default, outline, secondary, ghost, destructive, link; `size`: default, sm, xs, icon, icon-sm), `Input`, `Textarea`, `NativeSelect`, `Badge`, `Card`, `ScrollArea`, `Dialog`, `DialogContent`, `DialogTitle`, `DialogDescription` |
| `icons` | Every lucide icon component by name, e.g. `icons.GitPullRequest` |
| `get(addr, depth = 1)` | Reads a node: `Promise<Node>` |
| `watch(addr, depth, onNode)` | Calls `onNode(node)` now and whenever the node changes; returns the function that stops watching |
| `useNode(addr, depth = 1)` | React hook: the node, or `null` while it loads or when it fails, re-rendered on change |
| `do(ops)` | Runs actions: `ops` is `[{target, action, args?}]`, returns `Promise<[{ok, addr, message?, data?}]>`. Recorded as actor `plugin:<name>` |
| `focus(addr, task?)` / `useFocus()` | Makes every panel follow a node (pass a session's task with it) / React hook returning the focused address |
| `registerPanel({id, title, icon?, component, defaultPosition?})` | Adds a dock panel. `icon` is a lucide name or component; `defaultPosition` is `left`, `center`, `right` (default) or `right-bottom` |
| `registerCommand({id, title, group?, run})` | Adds a ⌘K command |
| `openPanel(id)` | Shows a panel |
| `notify(text, level = "info")` | Shows a toast; `level` is info, success, warning or error |
| `theme.mode()` / `theme.token(name)` | The current colour scheme (`dark` or `light`) / the current value of a CSS token such as `primary` |

`Node` is `{addr, kind, title, fields, children: [{addr, kind, title, status?, children?}], actions: [{name, help, args}]}`, the same JSON as `layer8://` resources. With depth 2, `app` lists projects with their tasks; a `task` node has `branch`, `status`, `pr` (`{number, url, state, checks…}` once opened) and `worktree` fields.

## Styling

Use Tailwind classes with the theme tokens (`bg-card`, `text-muted-foreground`, `border`, `text-primary`, `text-ok`, `text-wait`, `text-bad`), never hard-coded colours, so the panel follows the user's theme. Fill the panel with `h-full`.

## Errors

A plugin that throws while loading, or a panel that throws while rendering, shows an error card in its place; the rest of the window keeps working. The `plugins` field of `ui:plugins` gives each plugin's `status` (`pending`, `loaded` or `failed`), its `error`, and the `panels` and `commands` it registered. Plugins register panels and ⌘K commands only; they cannot add node actions.
