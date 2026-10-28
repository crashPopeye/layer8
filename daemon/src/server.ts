import { createServer, type IncomingMessage, type ServerResponse } from "node:http";
import { timingSafeEqual } from "node:crypto";
import { WebSocketServer, type WebSocket } from "ws";
import { bus } from "./bus.js";
import type { Store } from "./db.js";
import { launchDetached } from "./exec.js";
import { editorLaunch, terminalLaunch } from "./launch.js";
import { Linear } from "./linear.js";
import { workflowPrompt } from "./prompts.js";
import type { SessionManager } from "./sessions.js";
import { addWorkspace, listArtifacts, readArtifact, type TaskService } from "./tasks.js";
import * as tmux from "./tmux.js";
import type { AppState, Config, ServerPush, SessionKind } from "./types.js";

// Origins of the Tauri webview (macOS/Linux, Windows) and the Vite dev server.
const allowedOrigins = new Set(["tauri://localhost", "http://tauri.localhost", "http://localhost:1420"]);

export class HttpError extends Error {
  constructor(
    readonly status: number,
    message: string,
  ) {
    super(message);
  }
}

type Deps = {
  store: Store;
  sessions: SessionManager;
  tasks: TaskService;
  config: () => Config;
  saveConfig: (c: Config) => void;
  token: string;
};

type Handler = (ctx: { params: string[]; body: any; query: URLSearchParams }) => Promise<unknown> | unknown;

const sessionKinds: SessionKind[] = ["research", "plan", "implement", "chat"];

export function state(store: Store): AppState {
  return {
    workspaces: store.listWorkspaces(),
    tasks: store.listTasks(),
    sessions: store.listSessions(),
    approvals: store.pendingApprovals(),
  };
}

function tokenMatches(given: string | null | undefined, token: string): boolean {
  if (!given) return false;
  const a = Buffer.from(given);
  const b = Buffer.from(token);
  return a.length === b.length && timingSafeEqual(a, b);
}

export function createApp(deps: Deps) {
  const { store, sessions, tasks, config } = deps;

  const mustTask = (id: string) => {
    const t = store.getTask(id);
    if (!t) throw new HttpError(404, "unknown task");
    return t;
  };
  const mustSession = (id: string) => {
    const s = store.getSession(id);
    if (!s) throw new HttpError(404, "unknown session");
    return s;
  };
  const linear = () => {
    const key = config().linearApiKey;
    if (!key) throw new HttpError(400, "Linear API key is not configured");
    return new Linear(key);
  };
  const open = (l: { file: string; args: string[] }, cwd?: string) => {
    launchDetached(l.file, l.args, cwd);
    return { ok: true };
  };

  const routes: [string, RegExp, Handler][] = [
    ["GET", /^\/api\/state$/, () => state(store)],

    ["GET", /^\/api\/config$/, () => ({ ...config(), linearApiKey: config().linearApiKey ? "set" : null })],
    [
      "PUT",
      /^\/api\/config$/,
      ({ body }) => {
        const next = { ...config(), ...body };
        // The app never receives the key back, so "set" means "keep the current one".
        if (body.linearApiKey === "set") next.linearApiKey = config().linearApiKey;
        deps.saveConfig(next);
        return { ok: true };
      },
    ],

    [
      "POST",
      /^\/api\/workspaces$/,
      async ({ body }) => {
        const ws = await addWorkspace(store, String(body.path ?? ""));
        bus.emit("changed");
        return ws;
      },
    ],
    [
      "DELETE",
      /^\/api\/workspaces\/([\w-]+)$/,
      ({ params }) => {
        store.deleteWorkspace(params[0]);
        bus.emit("changed");
        return { ok: true };
      },
    ],

    ["GET", /^\/api\/linear\/issues$/, ({ query }) => {
      const q = query.get("q")?.trim();
      return q ? linear().search(q) : linear().myIssues();
    }],

    [
      "POST",
      /^\/api\/tasks$/,
      async ({ body }) => {
        const task = await tasks.create({
          workspaceId: body.workspaceId,
          linearIssue: body.linearIssue,
          title: body.title,
        });
        bus.emit("changed");
        return task;
      },
    ],
    [
      "POST",
      /^\/api\/tasks\/([\w-]+)\/archive$/,
      async ({ params, body }) => {
        await tasks.archive(params[0], !!body.removeWorktree);
        bus.emit("changed");
        return { ok: true };
      },
    ],
    ["GET", /^\/api\/tasks\/([\w-]+)\/artifacts$/, ({ params }) => listArtifacts(mustTask(params[0]))],
    [
      "GET",
      /^\/api\/tasks\/([\w-]+)\/artifacts\/([^/]+)$/,
      ({ params }) => ({ content: readArtifact(mustTask(params[0]), decodeURIComponent(params[1])) }),
    ],
    [
      "POST",
      /^\/api\/tasks\/([\w-]+)\/sessions$/,
      ({ params, body }) => {
        const task = mustTask(params[0]);
        const kind = body.kind as SessionKind;
        if (!sessionKinds.includes(kind)) throw new HttpError(400, "unknown session kind");
        const prompt = workflowPrompt(kind, task, String(body.prompt ?? ""));
        if (!prompt.trim()) throw new HttpError(400, "empty prompt");
        const title = body.title || (kind === "chat" ? String(body.prompt).slice(0, 60) : kind);
        return sessions.create(task.id, kind, title, prompt);
      },
    ],
    [
      "POST",
      /^\/api\/tasks\/([\w-]+)\/open$/,
      async ({ params, body }) => {
        const task = mustTask(params[0]);
        const cfg = config();
        if (body.target === "editor") return open(editorLaunch(cfg, task.worktreePath));
        const cmd = task.tmuxSession
          ? tmux.attachCommand(task.tmuxSession, task.worktreePath)
          : (process.env.SHELL ?? "/bin/zsh");
        return open(terminalLaunch(cfg, process.platform, task.worktreePath, cmd));
      },
    ],

    ["GET", /^\/api\/sessions\/([\w-]+)\/events$/, ({ params, query }) => {
      mustSession(params[0]);
      return store.listEvents(params[0], Number(query.get("after") ?? 0));
    }],
    [
      "POST",
      /^\/api\/sessions\/([\w-]+)\/messages$/,
      ({ params, body }) => {
        mustSession(params[0]);
        const text = String(body.text ?? "").trim();
        if (!text) throw new HttpError(400, "empty message");
        sessions.send(params[0], text);
        return { ok: true };
      },
    ],
    [
      "POST",
      /^\/api\/sessions\/([\w-]+)\/interrupt$/,
      async ({ params }) => {
        await sessions.interrupt(params[0]);
        return { ok: true };
      },
    ],
    [
      // Continue a session by hand: `claude --resume` in a new window of the task's tmux session.
      "POST",
      /^\/api\/sessions\/([\w-]+)\/open-terminal$/,
      async ({ params }) => {
        const s = mustSession(params[0]);
        const task = mustTask(s.taskId);
        if (!s.claudeSessionId) throw new HttpError(409, "session has not started yet");
        const resume = `claude --resume ${tmux.shellQuote(s.claudeSessionId)}`;
        const cfg = config();
        if (task.tmuxSession) {
          await tmux.ensureSession(task.tmuxSession, task.worktreePath, { LAYER8_TASK: task.slug });
          await tmux.newWindow(task.tmuxSession, task.worktreePath, `claude-${s.kind}`, resume);
          return open(terminalLaunch(cfg, process.platform, task.worktreePath, tmux.attachCommand(task.tmuxSession, task.worktreePath)));
        }
        return open(terminalLaunch(cfg, process.platform, task.worktreePath, resume));
      },
    ],
    [
      "POST",
      /^\/api\/approvals\/([\w-]+)$/,
      ({ params, body }) => {
        try {
          sessions.resolveApproval(params[0], !!body.allow, body.message);
        } catch (err) {
          throw new HttpError(409, (err as Error).message);
        }
        return { ok: true };
      },
    ],
  ];

  async function handle(req: IncomingMessage, res: ServerResponse): Promise<void> {
    const origin = req.headers.origin;
    if (origin && allowedOrigins.has(origin)) {
      res.setHeader("Access-Control-Allow-Origin", origin);
      res.setHeader("Vary", "Origin");
      res.setHeader("Access-Control-Allow-Headers", "authorization, content-type");
      res.setHeader("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE");
    }
    if (req.method === "OPTIONS") return void res.writeHead(204).end();

    const send = (status: number, body: unknown) => {
      res.writeHead(status, { "Content-Type": "application/json" });
      res.end(JSON.stringify(body));
    };

    // Rejecting foreign Host headers blocks DNS-rebinding; the token blocks other local pages.
    if (!isLocalHost(req.headers.host)) return send(403, { error: "forbidden host" });
    if (origin && !allowedOrigins.has(origin)) return send(403, { error: "forbidden origin" });
    const auth = req.headers.authorization?.replace(/^Bearer\s+/i, "");
    if (!tokenMatches(auth, deps.token)) return send(401, { error: "bad token" });

    const url = new URL(req.url ?? "/", "http://localhost");
    for (const [method, pattern, handler] of routes) {
      if (method !== req.method) continue;
      const m = pattern.exec(url.pathname);
      if (!m) continue;
      try {
        const body = method === "GET" ? {} : await readJson(req);
        send(200, (await handler({ params: m.slice(1), body, query: url.searchParams })) ?? { ok: true });
      } catch (err) {
        const status = err instanceof HttpError ? err.status : 500;
        send(status, { error: err instanceof Error ? err.message : String(err) });
      }
      return;
    }
    send(404, { error: "not found" });
  }

  const server = createServer((req, res) => void handle(req, res));
  const wss = new WebSocketServer({ noServer: true });

  server.on("upgrade", (req, socket, head) => {
    const url = new URL(req.url ?? "/", "http://localhost");
    const origin = req.headers.origin;
    const ok =
      url.pathname === "/ws" &&
      isLocalHost(req.headers.host) &&
      (!origin || allowedOrigins.has(origin)) &&
      tokenMatches(url.searchParams.get("token"), deps.token);
    if (!ok) {
      socket.write("HTTP/1.1 401 Unauthorized\r\n\r\n");
      return socket.destroy();
    }
    wss.handleUpgrade(req, socket, head, (ws) => {
      push(ws, { type: "state", state: state(store) });
    });
  });

  const push = (ws: WebSocket, msg: ServerPush) => ws.readyState === ws.OPEN && ws.send(JSON.stringify(msg));
  const broadcast = (msg: ServerPush) => wss.clients.forEach((ws) => push(ws, msg));

  // Coalesce bursts of changes (a session start touches several rows) into one push.
  let scheduled = false;
  bus.on("changed", () => {
    if (scheduled) return;
    scheduled = true;
    setImmediate(() => {
      scheduled = false;
      broadcast({ type: "state", state: state(store) });
    });
  });
  bus.on("event", (event) => broadcast({ type: "event", event }));

  return server;
}

function isLocalHost(host: string | undefined): boolean {
  if (!host) return false;
  const name = host.replace(/:\d+$/, "");
  return name === "127.0.0.1" || name === "localhost" || name === "[::1]";
}

async function readJson(req: IncomingMessage): Promise<any> {
  const chunks: Buffer[] = [];
  let size = 0;
  for await (const chunk of req) {
    size += chunk.length;
    if (size > 5 * 1024 * 1024) throw new HttpError(413, "body too large");
    chunks.push(chunk);
  }
  if (!chunks.length) return {};
  try {
    return JSON.parse(Buffer.concat(chunks).toString("utf8"));
  } catch {
    throw new HttpError(400, "invalid JSON");
  }
}
