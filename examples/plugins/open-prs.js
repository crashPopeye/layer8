/** Open PRs: a panel listing the open pull requests of every active task and sub-task, across projects. */
export default function setup(sdk) {
  const { React, ui } = sdk;
  const h = React.createElement;

  async function openPRs() {
    const app = await sdk.get("app", 2);
    const active = (children) => children.filter((c) => c.kind === "task" && c.status === "active");
    const found = [];
    const visit = async (addr) => {
      const task = await sdk.get(addr, 1);
      if (task.fields?.pr?.state === "open") found.push(task);
      await Promise.all(active(task.children).map((c) => visit(c.addr)));
    };
    const roots = app.children.filter((c) => c.kind === "project").flatMap((p) => active(p.children ?? []));
    await Promise.all(roots.map((t) => visit(t.addr)));
    return found.sort((a, b) => a.fields.pr.number - b.fields.pr.number);
  }

  const checks = { success: "text-ok", failure: "text-bad", pending: "text-wait" };

  function Row({ task }) {
    const pr = task.fields.pr;
    return h(
      ui.Button,
      { variant: "ghost", className: "h-auto w-full justify-start gap-2 px-2 py-1.5", onClick: () => sdk.focus(task.addr) },
      h("span", { className: "font-mono text-muted-foreground" }, `#${pr.number}`),
      h("span", { className: "truncate" }, task.title),
      h("span", { className: "flex-1" }),
      pr.draft && h(ui.Badge, null, "draft"),
      pr.checksState && h("span", { className: `text-[11px] ${checks[pr.checksState] ?? ""}` }, pr.checksState),
    );
  }

  function OpenPRs() {
    const [prs, setPRs] = React.useState(null);
    const [error, setError] = React.useState(null);
    const refresh = React.useCallback(() => openPRs().then(setPRs, (e) => setError(e.message)), []);
    React.useEffect(() => {
      const stop = sdk.watch("app", 1, refresh);
      const timer = setInterval(refresh, 60_000);
      return () => {
        stop();
        clearInterval(timer);
      };
    }, [refresh]);

    if (error) return h("p", { className: "p-3 text-bad" }, error);
    if (!prs) return h("p", { className: "p-3 text-muted-foreground" }, "Loading…");
    return h(
      ui.ScrollArea,
      { className: "h-full p-1" },
      prs.length === 0
        ? h("p", { className: "p-2 text-muted-foreground" }, "No open pull requests.")
        : prs.map((task) => h(Row, { key: task.addr, task })),
    );
  }

  sdk.registerPanel({ id: "open-prs", title: "Open PRs", icon: "GitPullRequest", component: OpenPRs, defaultPosition: "right" });
  sdk.registerCommand({ id: "open-prs.show", title: "Show open PRs", run: () => sdk.openPanel("open-prs") });
}
