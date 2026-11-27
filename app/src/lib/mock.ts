import type { Addr, Client, L8Event, Node, Op, Result } from "./types";

const addProject = {
  name: "add-project",
  help: "Add a git repository as a project",
  args: { type: "object", properties: { path: { type: "string" } }, required: ["path"] },
};

/** Returns an in-memory Client serving the `app` and `project` kinds, for running without a daemon. */
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
        fields: {},
        children: [...projects.keys()].map((name) => ({ addr: `project:${name}`, kind: "project", title: name })),
        actions: [addProject],
      };
    }
    const name = addr.startsWith("project:") ? addr.slice("project:".length) : "";
    const path = projects.get(name);
    if (path === undefined) throw new Error(`no node at ${addr}`);
    return { addr, kind: "project", title: name, fields: { path }, children: [], actions: [] };
  }

  function run(op: Op): Result {
    if (op.target !== "app" || op.action !== "add-project") {
      return { ok: false, message: `${op.action} is not an action of ${op.target}` };
    }
    const path = String((op.args as { path?: string } | undefined)?.path ?? "").replace(/\/+$/, "");
    const name = path.split("/").pop();
    if (!name) return { ok: false, message: "path is required" };
    if (projects.has(name)) return { ok: false, message: `project ${name} already exists` };
    projects.set(name, path);
    return { ok: true, addr: `project:${name}` };
  }

  return {
    mode: "mock",
    label: "mock",
    getNode: async (addr) => get(addr),
    async doOps(ops) {
      const results = ops.map(run);
      if (results.some((r) => r.ok)) emit(["app"]);
      return results;
    },
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
  };
}
