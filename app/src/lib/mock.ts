import type { Addr, Client, L8Event, Node, Op, Result } from "./types";

const addProject = {
  name: "add-project",
  help: "Add a local git repository as a project",
  args: {
    type: "object",
    required: ["path"],
    properties: { path: { type: "string" }, name: { type: "string" } },
  },
};

/** Returns an in-memory Client serving the `app` and `project` kinds the way layer8d does, for running without a daemon. */
export function mockClient(): Client {
  const projects = new Map<string, string>();
  const listeners = new Set<(e: L8Event) => void>();
  const emit = (addrs: Addr[]) => listeners.forEach((l) => l({ type: "node", data: { addrs } }));

  function get(addr: Addr): Node {
    if (addr === "app") {
      return {
        addr,
        kind: "app",
        title: "layer8",
        fields: { version: "mock" },
        children: [...projects.keys()].sort().map((name) => ({ addr: `project:${name}`, kind: "project", title: name })),
        actions: [addProject],
      };
    }
    const name = addr.startsWith("project:") ? addr.slice("project:".length) : "";
    const path = projects.get(name);
    if (path === undefined) throw new Error(`not found: ${addr}`);
    return { addr, kind: "project", title: name, fields: { path, defaultBranch: "main" }, children: [], actions: [] };
  }

  function run(op: Op): Result {
    const fail = (message: string) => ({ ok: false, addr: op.target, message });
    if (op.target !== "app" || op.action !== "add-project") return fail(`${op.target} has no action "${op.action}"`);
    const args = (op.args ?? {}) as { path?: string; name?: string };
    const path = String(args.path ?? "").replace(/\/+$/, "");
    if (!path.startsWith("/")) return fail(`path must be absolute: "${path}"`);
    const name = args.name || path.split("/").pop()!;
    if (projects.has(name)) return fail(`project "${name}" already exists`);
    projects.set(name, path);
    return { ok: true, addr: `project:${name}`, message: `added project ${name}` };
  }

  return {
    mode: "mock",
    label: "mock",
    getNode: async (addr) => get(addr),
    getJSON: async (path) => {
      throw new Error(`${path.split("?")[0]} needs a running layer8d`);
    },
    async doOps(ops) {
      const results = ops.map(run);
      results.filter((r) => r.ok).forEach((r) => emit(["app", r.addr]));
      return results;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}
