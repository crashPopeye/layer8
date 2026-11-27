import { mockSessions, startSession } from "./mock-sessions";
import type { Action, Addr, Client, L8Event, Node, Op, Result, Summary } from "./types";

const schema = (required: string[], properties: Record<string, unknown>) => ({ type: "object", required, properties });

const actions: Record<string, Action> = {
  "add-project": {
    name: "add-project",
    help: "Add a local git repository as a project",
    args: schema(["path"], { path: { type: "string" }, name: { type: "string" } }),
  },
  "create-task": {
    name: "create-task",
    help: "Create a task with its own branch, worktree and artifacts folder",
    args: schema(["title"], {
      title: { type: "string" },
      slug: { type: "string", description: "Short id for the address, branch and folders, derived from the title by default" },
      base: { type: "string", description: "Branch to start from, defaults to the project's default branch" },
      branch: { type: "string", description: "Branch name, defaults to the slug" },
    }),
  },
  "create-subtask": {
    name: "create-subtask",
    help: "Create a child task stacked on this one",
    args: schema(["name"], {
      name: { type: "string", description: "Short name, becomes the last part of the address" },
      base: { type: "string", description: "Sibling sub-task name or branch to stack on, defaults to this task's branch" },
    }),
  },
  done: { name: "done", help: "Mark the task done; its worktree stays" },
  reopen: { name: "reopen", help: "Make the task active again, restoring its worktree" },
  archive: {
    name: "archive",
    help: "Archive the task and its sub-tasks, removing their worktrees and keeping branches and artifacts",
  },
  open: {
    name: "open",
    help: "Open the worktree in the terminal, the editor or Finder",
    args: schema(["target"], { target: { enum: ["terminal", "editor", "finder"] } }),
  },
  write: {
    name: "write",
    help: "Replace the artifact's content, creating it if needed",
    args: schema(["content"], { content: { type: "string" } }),
  },
  delete: { name: "delete", help: "Delete the artifact" },
};

const taskActions: Record<string, string[]> = {
  active: ["create-subtask", "done", "archive", "open"],
  done: ["reopen", "archive", "open"],
  archived: ["reopen"],
};

const mediaTypes: Record<string, string> = {
  md: "text/markdown",
  html: "text/html",
  svg: "image/svg+xml",
  png: "image/png",
  json: "application/json",
  go: "text/plain",
};

const mediaType = (name: string) => mediaTypes[name.split(".").pop()!.toLowerCase()] ?? "text/plain";

interface Task {
  slug: string;
  title: string;
  project: string;
  branch: string;
  base: string;
  status: string;
}

const demoFiles: Record<string, string> = {
  "README.md": "# api-gateway\n\nThe public HTTP entry point.\n\n```sh\ngo run ./cmd/gateway\n```\n",
  "go.mod": "module example.com/api-gateway\n\ngo 1.26\n",
  "cmd/gateway/main.go":
    'package main\n\nimport "example.com/api-gateway/internal/http"\n\nfunc main() {\n\thttp.Serve(":8080")\n}\n',
  "internal/http/ratelimit.go":
    'package http\n\n// Limit wraps next with a token bucket per API key.\nfunc Limit(next Handler) Handler {\n\treturn func(w ResponseWriter, r *Request) {\n\t\tif !bucket(r).Allow() {\n\t\t\tw.Header().Set("Retry-After", "1")\n\t\t\tError(w, "slow down", 429)\n\t\t\treturn\n\t\t}\n\t\tnext(w, r)\n\t}\n}\n',
};

const slugify = (s: string) =>
  s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-|-$/g, "")
    .slice(0, 48) || "task";

const addrOf = (slug: string): Addr => `task:${slug.replaceAll("--", "/")}`;
const slugOf = (addr: Addr) => addr.slice("task:".length).replaceAll("/", "--");
const rootOf = (slug: string) => slug.split("--")[0];
const parentOf = (t: Task): Addr =>
  t.slug.includes("--") ? addrOf(t.slug.slice(0, t.slug.lastIndexOf("--"))) : `project:${t.project}`;

/** Returns an in-memory Client serving the app, project, task and artifact kinds the way layer8d does, after applying seed. */
export function mockClient(seed: Op[] = []): Client {
  const projects = new Map<string, string>();
  const tasks = new Map<string, Task>();
  const artifacts = new Map<string, { content: string; modified: string }>();
  const listeners = new Set<(e: L8Event) => void>();
  const emit = (addrs: Addr[]) => listeners.forEach((l) => l({ type: "node", data: { addrs: [...new Set(addrs)] } }));
  const sessions = mockSessions((e) => listeners.forEach((l) => l(e)));

  const taskList = (where: (t: Task) => boolean): Summary[] =>
    [...tasks.values()].filter(where).map((t) => ({ addr: addrOf(t.slug), kind: "task", title: t.title, status: t.status }));
  const artifactList = (root: string): Summary[] =>
    [...artifacts.keys()]
      .filter((k) => k.startsWith(root + "/"))
      .sort()
      .map((k) => ({ addr: `artifact:${k}`, kind: "artifact", title: k.slice(root.length + 1) }));

  function get(addr: Addr): Node {
    const [kind, path = ""] = addr.split(/:(.*)/s);
    if (addr === "app") {
      const children = [...projects.keys()].sort().map((name) => ({ addr: `project:${name}`, kind: "project", title: name }));
      return { addr, kind: "app", title: "layer8", fields: { version: "mock" }, children, actions: [actions["add-project"]] };
    }
    if (kind === "project" && projects.has(path)) {
      const children = taskList((t) => t.project === path && !t.slug.includes("--"));
      return {
        addr,
        kind,
        title: path,
        fields: { path: projects.get(path), defaultBranch: "main" },
        children,
        actions: [actions["create-task"]],
      };
    }
    const session = kind === "session" ? sessions.get(addr) : undefined;
    if (session) return session;
    const t = kind === "task" ? tasks.get(slugOf(addr)) : undefined;
    if (t) {
      const root = rootOf(t.slug);
      const subs = taskList((s) => s.slug.startsWith(t.slug + "--") && !s.slug.slice(t.slug.length + 2).includes("--"));
      return {
        addr,
        kind,
        title: t.title,
        fields: {
          slug: t.slug,
          branch: t.branch,
          base: t.base,
          status: t.status,
          worktree: `/mock/worktrees/${t.project}/${t.slug}`,
          artifacts: `/mock/tasks/${root}`,
          artifactFiles: artifactList(root),
          project: `project:${t.project}`,
          parent: parentOf(t),
          createdBy: "user",
        },
        children: [...subs, ...sessions.children(addr), ...(t.slug === root ? artifactList(root) : [])],
        actions: [...taskActions[t.status].map((a) => actions[a]), ...(t.status === "active" ? [startSession] : [])],
      };
    }
    if (kind === "artifact" && tasks.has(path.split("/")[0])) {
      const a = artifacts.get(path);
      const rel = path.slice(path.indexOf("/") + 1);
      const fields: Record<string, unknown> = { task: addrOf(path.split("/")[0]), path: `/mock/tasks/${path}`, exists: !!a };
      if (a)
        Object.assign(fields, {
          size: a.content.length,
          modified: a.modified,
          type: mediaType(rel),
          url: `/api/artifact?addr=${encodeURIComponent(addr)}`,
        });
      return { addr, kind, title: rel, fields, children: [], actions: a ? [actions.write, actions.delete] : [actions.write] };
    }
    throw new Error(`not found: ${addr}`);
  }

  function summaries(addr: Addr, depth: number): Summary[] {
    if (depth <= 0) return [];
    return get(addr).children.map((c) => {
      const children = summaries(c.addr, depth - 1);
      return children.length ? { ...c, children } : c;
    });
  }

  function create(t: Task): Result {
    if (tasks.has(t.slug)) return { ok: false, addr: addrOf(t.slug), message: `task ${addrOf(t.slug)} already exists` };
    tasks.set(t.slug, t);
    return { ok: true, addr: addrOf(t.slug), message: `created ${addrOf(t.slug)} on branch ${t.branch} from ${t.base}` };
  }

  function apply(n: Node, action: string, args: Record<string, any>): Result {
    const ok = (message: string, addr = n.addr) => ({ ok: true, addr, message });
    const t = tasks.get(slugOf(n.addr));
    switch (action) {
      case "add-project": {
        const path = String(args.path ?? "").replace(/\/+$/, "");
        if (!path.startsWith("/")) return { ok: false, addr: n.addr, message: `path must be absolute: "${path}"` };
        const name = args.name || path.split("/").pop()!;
        if (projects.has(name)) return { ok: false, addr: n.addr, message: `project "${name}" already exists` };
        projects.set(name, path);
        return ok(`added project ${name}`, `project:${name}`);
      }
      case "create-task": {
        const slug = slugify(args.slug || args.title);
        return create({
          slug,
          title: args.title,
          project: n.title,
          branch: args.branch || slug,
          base: args.base || "main",
          status: "active",
        });
      }
      case "create-subtask": {
        const slug = `${t!.slug}--${slugify(args.name)}`;
        const sibling = args.base && tasks.get(`${t!.slug}--${slugify(args.base)}`);
        const base = sibling ? sibling.branch : args.base || t!.branch;
        return create({
          slug,
          title: args.name,
          project: t!.project,
          branch: slug.replaceAll("--", "-"),
          base,
          status: "active",
        });
      }
      case "done":
      case "reopen":
        t!.status = action === "done" ? "done" : "active";
        return ok(`${n.addr} is ${t!.status}`);
      case "archive": {
        const subtree = [...tasks.values()].filter((s) => s.slug === t!.slug || s.slug.startsWith(t!.slug + "--"));
        subtree.forEach((s) => (s.status = "archived"));
        return ok(`archived ${subtree.length} task(s); branches and artifacts kept`);
      }
      case "open":
        return ok(`opened ${n.fields!.worktree} in ${args.target}`);
      case "write":
        artifacts.set(n.addr.slice("artifact:".length), { content: args.content, modified: new Date().toISOString() });
        return ok(`wrote ${args.content.length} bytes to ${n.addr}`);
      case "delete":
        artifacts.delete(n.addr.slice("artifact:".length));
        return ok(`deleted ${n.addr}`);
    }
    return sessions.run(n, action, args) ?? { ok: false, addr: n.addr, message: `${n.addr} has no action "${action}"` };
  }

  function run(op: Op): Result {
    let n: Node;
    try {
      n = get(op.target);
    } catch (e) {
      return { ok: false, addr: op.target, message: (e as Error).message };
    }
    if (!n.actions.some((a) => a.name === op.action))
      return { ok: false, addr: op.target, message: `${op.target} has no action "${op.action}"` };
    const r = apply(n, op.action, (op.args ?? {}) as Record<string, any>);
    if (r.ok) emit(["app", n.addr, r.addr, ...[...projects.keys()].map((p) => `project:${p}`), ...[...tasks.keys()].map(addrOf)]);
    return r;
  }

  function raw(path: string): Blob {
    const url = new URL(path, "http://mock");
    if (url.pathname === "/api/artifact") {
      const key = (url.searchParams.get("addr") ?? "").slice("artifact:".length);
      const a = artifacts.get(key);
      if (!a) throw new Error("not found");
      return new Blob([a.content], { type: mediaType(key) });
    }
    if (url.pathname === "/api/file" && tasks.has(slugOf(url.searchParams.get("task") ?? ""))) {
      const rel = (url.searchParams.get("path") ?? ".").replace(/^\.$/, "");
      if (rel in demoFiles) return new Blob([demoFiles[rel]], { type: mediaType(rel) });
      const prefix = rel ? rel + "/" : "";
      const entries = new Map<string, { name: string; dir: boolean; size: number }>();
      for (const [p, content] of Object.entries(demoFiles)) {
        if (!p.startsWith(prefix)) continue;
        const [name, ...rest] = p.slice(prefix.length).split("/");
        entries.set(name, { name, dir: rest.length > 0, size: rest.length ? 0 : content.length });
      }
      if (entries.size === 0) throw new Error("not found");
      const list = [...entries.values()].sort((a, b) => a.name.localeCompare(b.name));
      return new Blob([JSON.stringify(list)], { type: "application/json" });
    }
    throw new Error(`not found: ${path}`);
  }

  seed.forEach(run);

  return {
    mode: "mock",
    label: "mock",
    getNode: async (addr, depth = 1) => ({ ...get(addr), children: summaries(addr, depth) }),
    getJSON: async <T>(path: string) => {
      if (path.startsWith("/api/files?")) return { files: Object.keys(demoFiles).sort(), truncated: false } as T;
      const body = sessions.json(path);
      if (body === undefined) throw new Error(`${path.split("?")[0]} needs a running layer8d`);
      return body as T;
    },
    doOps: async (ops) => ops.map(run),
    raw: async (path) => raw(path),
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}

const plan = `# Plan: rate-limit uploads

Wrap upload routes with a token bucket keyed by API key. A 429 carries \`Retry-After\`.

| PR | Scope | Base |
|---|---|---|
| pr1-store | bucket store | main |
| pr2-middleware | middleware | pr1-store |
| pr3-docs | docs | pr2-middleware |

\`\`\`mermaid
flowchart LR
  R[request] --> L{bucket.Allow}
  L -- yes --> N[next]
  L -- no --> E[429 Retry-After]
\`\`\`

\`\`\`go
if !b.Allow(key) {
	w.Header().Set("Retry-After", "1")
	http.Error(w, "slow down", 429)
	return
}
\`\`\`

- [x] research
- [ ] implement
`;

const mockup = `<!doctype html><html><body style="font:14px system-ui;background:#111;color:#eee;padding:16px">
<h3>Upload limits</h3><p>Remaining: <b id="n">10</b> uploads this minute.</p>
<button onclick="n.textContent=Math.max(0,n.textContent-1)">Upload</button></body></html>`;

/** A small demo tree so the panels have something to show in mock mode. */
export const demoSeed: Op[] = [
  { target: "app", action: "add-project", args: { path: "/Users/me/src/api-gateway" } },
  { target: "app", action: "add-project", args: { path: "/Users/me/src/web-app" } },
  { target: "project:api-gateway", action: "create-task", args: { title: "ENG-470 Retry webhooks", slug: "eng-470" } },
  { target: "task:eng-470", action: "done" },
  { target: "project:api-gateway", action: "create-task", args: { title: "ENG-482 Rate-limit uploads", slug: "eng-482" } },
  { target: "task:eng-482", action: "create-subtask", args: { name: "pr1-store" } },
  { target: "task:eng-482", action: "create-subtask", args: { name: "pr2-middleware", base: "pr1-store" } },
  { target: "task:eng-482", action: "create-subtask", args: { name: "pr3-docs", base: "pr2-middleware" } },
  { target: "task:eng-482/pr1-store", action: "done" },
  {
    target: "task:eng-482/pr2-middleware",
    action: "start-session",
    args: {
      prompt: "/implement-plan pr2 — follow the plan, stop before pushing.",
      title: "Implement",
      model: "opus",
      effort: "high",
      permissionMode: "acceptEdits",
    },
  },
  { target: "project:web-app", action: "create-task", args: { title: "ENG-501 Upload progress UI", slug: "eng-501" } },
  {
    target: "artifact:eng-482/01-ticket.md",
    action: "write",
    args: { content: "# ENG-482 Rate-limit uploads\n\nUploads need a per-key limit.\n" },
  },
  { target: "artifact:eng-482/05-plan.md", action: "write", args: { content: plan } },
  { target: "artifact:eng-482/mockup-limits.html", action: "write", args: { content: mockup } },
];
