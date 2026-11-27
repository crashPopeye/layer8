import type { Action, Addr, L8Event, Node, Result, SessionEvent, Summary } from "./types";

const obj = (properties: Record<string, unknown>, required: string[] = []) => ({ type: "object", required, properties });

/** The session actions layer8d declares, with the statuses each is offered in. */
const sessionActions: [Action, (s: MockSession) => boolean][] = [
  [
    {
      name: "say",
      help: "Send a message, resuming the agent if needed",
      args: obj({ text: { type: "string" }, files: { type: "array" }, images: { type: "array" } }, ["text"]),
    },
    () => true,
  ],
  [{ name: "interrupt", help: "Stop the current turn" }, (s) => ["running", "waiting"].includes(s.status)],
  [{ name: "allow", help: "Allow a pending tool call (user only)", args: obj({ approval: { type: "integer" }, always: { type: "boolean" } }) }, (s) => s.status === "waiting"],
  [{ name: "deny", help: "Deny a pending tool call (user only)", args: obj({ approval: { type: "integer" }, message: { type: "string" } }) }, (s) => s.status === "waiting"],
  [{ name: "close", help: "End the session and its agent process" }, (s) => s.status !== "done"],
  [{ name: "set", help: "Change model, effort or permission mode from the next turn", args: obj({ model: { type: "string" }, effort: { type: "string" }, permissionMode: { type: "string" } }) }, () => true],
  [{ name: "fork", help: "Copy this conversation into a new session" }, (s) => s.events.length > 0],
  [{ name: "open-terminal", help: "Continue this conversation in the terminal" }, (s) => ["idle", "done", "failed"].includes(s.status) && s.events.length > 0],
];

/** The start-session action tasks offer. */
export const startSession: Action = {
  name: "start-session",
  help: "Start an agent session in this task's worktree",
  args: obj({
    prompt: { type: "string" },
    skill: { type: "string" },
    harness: { enum: ["claude"] },
    model: { type: "string" },
    effort: { type: "string" },
    permissionMode: { type: "string" },
    title: { type: "string" },
  }),
};

const capabilities = {
  commands: [
    { name: "research", description: "Document the codebase for this task", plugin: "layer8" },
    { name: "layer8:plan", description: "Write the implementation plan and propose sub-tasks", plugin: "layer8" },
    { name: "review", description: "Review the current diff for bugs" },
    { name: "rpi:review-artifact-comments", description: "Apply your comments to an artifact", plugin: "rpi" },
    { name: "compact", description: "Clear history but keep a summary", builtin: true },
  ],
  models: [
    { value: "opus", displayName: "Opus", efforts: ["low", "medium", "high", "xhigh", "max"] },
    { value: "sonnet", displayName: "Sonnet", efforts: ["low", "medium", "high"] },
    { value: "haiku", displayName: "Haiku" },
  ],
  agents: [],
  mcpServers: [{ name: "layer8", status: "connected" }],
};

interface MockSession {
  id8: string;
  task: Addr;
  title: string;
  model: string;
  effort: string;
  permissionMode: string;
  status: string;
  cost: number;
  events: SessionEvent[];
  approval: { id: number; toolUseId: string } | null;
  queued: number;
  timers: ReturnType<typeof setTimeout>[];
}

/** A fake session kind for mock mode: each message plays a short scripted turn that streams, edits, nests a subagent and asks for approval. */
export function mockSessions(send: (e: L8Event) => void, delay = 120) {
  const sessions = new Map<string, MockSession>();
  let nextEvent = 1;
  let nextApproval = 1;
  const addr = (s: MockSession) => `session:${s.id8}`;

  const changed = (s: MockSession) => send({ type: "node", data: { addrs: [addr(s), s.task] } });
  const status = (s: MockSession, st: string) => {
    s.status = st;
    changed(s);
  };
  function emit(s: MockSession, type: SessionEvent["type"], data: Record<string, unknown>, partial = false) {
    const e: SessionEvent = partial ? { type, partial, data } : { id: nextEvent++, type, at: new Date().toISOString(), data };
    if (!partial) s.events.push(e);
    send({ type: "session", data: { session: s.id8, events: [e] } });
  }
  function play(s: MockSession, steps: (() => void)[]) {
    steps.forEach((step, i) => s.timers.push(setTimeout(step, delay * (i + 1))));
  }
  const stream = (s: MockSession, type: "text" | "thinking", text: string, parent?: string) => {
    const extra = parent ? { parentToolUseId: parent } : {};
    const words = text.split(/(?<= )/);
    return [
      ...words.map((w) => () => emit(s, type, { text: w, ...extra }, true)),
      () => emit(s, type, { text, ...extra }),
    ];
  };

  function turn(s: MockSession) {
    const n = nextEvent;
    status(s, "running");
    play(s, [
      ...stream(s, "thinking", "The plan says to wrap upload routes with a token bucket."),
      ...stream(s, "text", "Reading the plan, then **editing** the middleware."),
      () => emit(s, "tool_use", { id: `task${n}`, name: "Task", input: { description: "Find the upload routes", subagent_type: "Explore" } }),
      () => emit(s, "tool_use", { id: `grep${n}`, name: "Grep", input: { pattern: "Upload", path: "internal" }, parentToolUseId: `task${n}` }),
      () => emit(s, "tool_result", { toolUseId: `grep${n}`, content: "internal/http/routes.go:12", parentToolUseId: `task${n}` }),
      () => emit(s, "text", { text: "Uploads are routed in `internal/http/routes.go`.", parentToolUseId: `task${n}` }),
      () => emit(s, "tool_result", { toolUseId: `task${n}`, content: [{ type: "text", text: "internal/http/routes.go:12" }] }),
      () =>
        emit(s, "tool_use", {
          id: `edit${n}`,
          name: "Edit",
          input: { file_path: "internal/http/ratelimit.go", old_string: "\treturn next(w, r)", new_string: '\tif !b.Allow(key) {\n\t\thttp.Error(w, "slow down", 429)\n\t\treturn\n\t}\n\treturn next(w, r)' },
        }),
      () => emit(s, "tool_result", { toolUseId: `edit${n}`, content: "The file has been updated." }),
      () => emit(s, "tool_use", { id: `bash${n}`, name: "Bash", input: { command: "go test ./internal/http/...", description: "Run the tests" } }),
      () => {
        s.approval = { id: nextApproval++, toolUseId: `bash${n}` };
        emit(s, "approval", { approval: s.approval.id, tool: "Bash", input: { command: "go test ./internal/http/..." }, toolUseId: `bash${n}`, decision: "pending" });
        status(s, "waiting");
      },
    ]);
  }

  function finish(s: MockSession, allowed: boolean, always = false) {
    const tool = s.approval!.toolUseId;
    emit(s, "approval", { approval: s.approval!.id, decision: allowed ? "allow" : "deny", always, by: "user" });
    s.approval = null;
    status(s, "running");
    play(s, [
      () => emit(s, "tool_result", { toolUseId: tool, content: allowed ? "ok  internal/http  0.41s" : "The user denied this tool call.", isError: !allowed }),
      ...stream(s, "text", allowed ? "Tests pass. The middleware returns 429 with `Retry-After`." : "Skipped the tests as asked."),
      () => end(s),
    ]);
  }

  function end(s: MockSession, interrupted = false) {
    s.cost += 0.42;
    emit(s, "result", {
      subtype: interrupted ? "error_during_execution" : "success",
      isError: interrupted,
      costUsd: s.cost,
      durationMs: 41000,
      numTurns: 6,
      usage: { input_tokens: 41200, output_tokens: 1830 },
    });
    if (s.queued > 0) {
      s.queued--;
      return turn(s);
    }
    status(s, "idle");
  }

  function say(s: MockSession, args: Record<string, unknown>, by = "user") {
    emit(s, "user", { ...args, by });
    if (["running", "waiting"].includes(s.status)) s.queued++;
    else turn(s);
  }

  function summary(s: MockSession): Summary {
    return { addr: addr(s), kind: "session", title: s.title, status: s.status };
  }

  function get(a: Addr): Node | undefined {
    const s = sessions.get(a.slice("session:".length));
    if (!s) return undefined;
    return {
      addr: a,
      kind: "session",
      title: s.title,
      fields: {
        id: s.id8,
        task: s.task,
        harness: "claude",
        model: s.model,
        effort: s.effort,
        permissionMode: s.permissionMode,
        status: s.status,
        costUsd: s.cost,
        contextTokens: 41200,
        capabilities: {
          mcpServers: [
            { name: "layer8", status: "connected" },
            { name: "claude.ai Linear", status: "connected" },
            { name: "github", status: "failed" },
          ],
          plugins: [{ name: "layer8" }, { name: "rpi" }],
        },
        approvals: s.approval ? [{ id: s.approval.id, tool: "Bash" }] : [],
      },
      children: [],
      actions: sessionActions.filter(([, when]) => when(s)).map(([a]) => a),
    };
  }

  function create(task: Addr, args: { model?: string; effort?: string; permissionMode?: string }, title: string): MockSession {
    const id8 = Math.random().toString(16).slice(2, 10).padEnd(8, "0");
    const s: MockSession = {
      id8,
      task,
      title,
      model: args.model ?? "",
      effort: args.effort ?? "",
      permissionMode: args.permissionMode ?? "",
      status: "idle",
      cost: 0,
      events: [],
      approval: null,
      queued: 0,
      timers: [],
    };
    sessions.set(id8, s);
    return s;
  }

  function run(n: Node, action: string, args: Record<string, any>): Result | undefined {
    const ok = (message: string, a = n.addr) => ({ ok: true, addr: a, message });
    if (action === "start-session") {
      const prompt = [args.skill && `/${args.skill}`, args.prompt].filter(Boolean).join(" ").trim();
      const s = create(n.addr, args, args.title || args.skill || prompt.split("\n")[0].slice(0, 60) || "Session");
      if (prompt) say(s, { text: prompt });
      changed(s);
      return ok(`started ${addr(s)} in ${n.addr}`, addr(s));
    }
    const s = sessions.get(n.addr.slice("session:".length));
    if (!s) return undefined;
    switch (action) {
      case "say":
        say(s, args);
        return ok(`sent to ${n.addr}`);
      case "allow":
      case "deny":
        finish(s, action === "allow", !!args.always);
        return ok(`${action === "allow" ? "allowed" : "denied"} approval ${args.approval ?? ""}`.trim());
      case "interrupt":
        s.timers.forEach(clearTimeout);
        s.timers = [];
        if (s.approval) emit(s, "approval", { approval: s.approval.id, decision: "deny", message: "interrupted", by: "user" });
        s.approval = null;
        end(s, true);
        return ok(`interrupted ${n.addr}`);
      case "close":
        s.timers.forEach(clearTimeout);
        status(s, "done");
        return ok(`closing ${n.addr}`);
      case "set":
        Object.assign(s, Object.fromEntries(Object.entries(args).filter(([k]) => ["model", "effort", "permissionMode"].includes(k))));
        changed(s);
        return ok("settings apply from the next turn");
      case "fork": {
        const f = create(s.task, s, `${s.title} (fork)`);
        f.events = s.events.map((e) => ({ ...e, id: nextEvent++ }));
        changed(f);
        return ok(`forked ${n.addr} into ${addr(f)}`, addr(f));
      }
      case "open-terminal":
        return ok(`opened ${n.addr} in the terminal`);
    }
    return undefined;
  }

  function json(path: string): unknown {
    const url = new URL(path, "http://mock");
    const events = /^\/api\/sessions\/(\w+)\/events$/.exec(url.pathname);
    if (events) {
      const s = sessions.get(events[1]);
      if (!s) throw new Error(`not found: session:${events[1]}`);
      const after = Number(url.searchParams.get("after") ?? 0);
      return { events: s.events.filter((e) => e.id! > after).slice(0, Number(url.searchParams.get("limit") ?? 500)) };
    }
    if (url.pathname === "/api/capabilities") return capabilities;
    return undefined;
  }

  return {
    get,
    run,
    json,
    children: (task: Addr) => [...sessions.values()].filter((s) => s.task === task).map(summary),
  };
}
