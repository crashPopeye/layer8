/** Linear: lists your open Linear issues with your own API key, opens them, and starts a layer8 task from one. */
export default function setup(sdk) {
  const { React, ui, icons } = sdk;
  const h = React.createElement;
  const keyName = "layer8.linear.apiKey";
  const issueFields = "id identifier title url branchName description state { name } team { name }";

  async function gql(key, query, variables) {
    const res = await fetch("https://api.linear.app/graphql", {
      method: "POST",
      headers: { "Content-Type": "application/json", Authorization: key },
      body: JSON.stringify({ query, variables }),
    });
    const body = await res.json().catch(() => ({}));
    if (!res.ok || body.errors) throw new Error(body.errors?.[0]?.message ?? `Linear answered ${res.status}`);
    return body.data;
  }

  const myIssues = (key) =>
    gql(
      key,
      `{ viewer { name assignedIssues(first: 50, orderBy: updatedAt, filter: {state: {type: {nin: ["completed", "canceled"]}}}) { nodes { ${issueFields} } } } }`,
    ).then((d) => ({ viewer: d.viewer.name, issues: d.viewer.assignedIssues.nodes }));

  const search = (key, term) =>
    gql(key, `query($term: String!) { searchIssues(term: $term, first: 20) { nodes { ${issueFields} } } }`, { term }).then(
      (d) => d.searchIssues.nodes,
    );

  function ticket(issue) {
    const lines = [`# ${issue.identifier} ${issue.title}`, "", `- **Linear:** [${issue.identifier}](${issue.url})`, `- **State:** ${issue.state.name}`];
    if (issue.description?.trim()) lines.push("", "## Description", "", issue.description.trim());
    return lines.join("\n") + "\n";
  }

  async function startTask(project, issue) {
    const [created] = await sdk.do([
      { target: project, action: "create-task", args: { title: issue.title, slug: issue.identifier, branch: issue.branchName || undefined } },
    ]);
    if (!created.ok) return sdk.notify(created.message, "error");
    const task = created.addr;
    await sdk.do([
      { target: task, action: "set-link", args: { url: issue.url, label: issue.identifier } },
      { target: `artifact:${task.slice("task:".length)}/01-ticket.md`, action: "write", args: { content: ticket(issue) } },
    ]);
    sdk.focus(task);
    sdk.notify(`Started ${task} from ${issue.identifier}`, "success");
  }

  function Connect({ onKey }) {
    const [key, setKey] = React.useState("");
    return h(
      "form",
      { className: "flex flex-col gap-2 p-3", onSubmit: (e) => (e.preventDefault(), onKey(key.trim())) },
      h("p", { className: "text-muted-foreground" }, "Paste a Linear personal API key (Linear → Settings → Security & access). It stays in this window."),
      h("div", { className: "flex gap-2" },
        h(ui.Input, { type: "password", value: key, placeholder: "lin_api_…", "aria-label": "Linear API key", onChange: (e) => setKey(e.target.value) }),
        h(ui.Button, { type: "submit", size: "sm", disabled: !key.trim() }, "Connect"),
      ),
    );
  }

  function Row({ issue, onStart }) {
    return h(
      "div",
      { className: "flex items-center gap-2 rounded-md px-1 py-0.5 hover:bg-muted" },
      h("span", { className: "shrink-0 font-mono text-[11px] text-muted-foreground" }, issue.identifier),
      h("span", { className: "min-w-0 flex-1 truncate", title: issue.title }, issue.title),
      h("span", { className: "shrink-0 text-[11px] text-muted-foreground" }, issue.state.name),
      h(ui.Button, { variant: "ghost", size: "icon-sm", title: "Start a task from this issue", onClick: () => onStart(issue) }, h(icons.Play)),
      h(ui.Button, { variant: "ghost", size: "icon-sm", title: "Open in Linear", onClick: () => sdk.openUrl(issue.url) }, h(icons.ExternalLink)),
    );
  }

  function Linear() {
    const [key, setKey] = React.useState(() => localStorage.getItem(keyName));
    const [mine, setMine] = React.useState(null);
    const [hits, setHits] = React.useState(null);
    const [query, setQuery] = React.useState("");
    const [error, setError] = React.useState(null);
    const [chosen, setChosen] = React.useState("");
    const projects = (sdk.useNode("app", 1)?.children ?? []).filter((c) => c.kind === "project");
    const project = chosen || projects[0]?.addr || "";

    React.useEffect(() => {
      if (!key) return;
      setError(null);
      myIssues(key).then(setMine, (e) => setError(e.message));
    }, [key]);

    const connect = (k) => (localStorage.setItem(keyName, k), setKey(k));
    const forget = () => (localStorage.removeItem(keyName), setKey(null), setMine(null), setHits(null));
    const find = (e) => {
      e.preventDefault();
      if (!query.trim()) return setHits(null);
      search(key, query.trim()).then(setHits, (err) => setError(err.message));
    };

    if (!key) return h(Connect, { onKey: connect });
    const issues = hits ?? mine?.issues;
    return h(
      "div",
      { className: "flex h-full flex-col gap-2 overflow-auto p-3" },
      h("div", { className: "flex items-center gap-2 text-muted-foreground" },
        h("span", { className: "truncate" }, mine ? `Linear · ${mine.viewer}` : "Linear"),
        h(ui.Button, { variant: "ghost", size: "xs", className: "ml-auto", onClick: forget }, "Forget key"),
      ),
      error && h("p", { className: "text-bad" }, error),
      h("label", { className: "grid gap-1 text-[12px] text-muted-foreground" }, "Start tasks in",
        h(ui.NativeSelect, { value: project, "aria-label": "Project", onChange: (e) => setChosen(e.target.value) },
          projects.map((p) => h("option", { key: p.addr, value: p.addr }, p.title)),
        ),
      ),
      h("form", { onSubmit: find },
        h(ui.Input, { value: query, placeholder: "Search issues ↵", "aria-label": "Search Linear issues", onChange: (e) => setQuery(e.target.value) }),
      ),
      h("h3", { className: "text-[11px] text-muted-foreground" }, hits ? `Results for “${query}”` : "My open issues"),
      !issues && !error && h("p", { className: "text-muted-foreground" }, "Loading…"),
      issues?.length === 0 && h("p", { className: "text-muted-foreground" }, "No issues."),
      issues?.map((issue) =>
        h(Row, { key: issue.id, issue, onStart: (i) => (project ? startTask(project, i) : sdk.notify("Add a project first", "warning")) }),
      ),
    );
  }

  sdk.registerPanel({ id: "linear", title: "Linear", icon: "Ticket", component: Linear, defaultPosition: "left" });
  sdk.registerCommand({ id: "linear.show", title: "Show Linear issues", run: () => sdk.openPanel("linear") });
}
