import type {
  AppState,
  Artifact,
  Config,
  LinearIssueSummary,
  ServerPush,
  Session,
  SessionEvent,
  SessionKind,
  Task,
  Workspace,
} from "@layer8/types";

export type DaemonInfo = { url: string; token: string };

const inTauri = () => "__TAURI_INTERNALS__" in window;

/** Inside Tauri the shell reads ~/.layer8/token; in a plain browser (dev) the user pastes it once. */
export async function discoverDaemon(): Promise<DaemonInfo | null> {
  if (inTauri()) {
    const { invoke } = await import("@tauri-apps/api/core");
    return invoke<DaemonInfo>("daemon_info");
  }
  try {
    const saved = localStorage.getItem("layer8.daemon");
    return saved ? (JSON.parse(saved) as DaemonInfo) : null;
  } catch {
    return null;
  }
}

export function rememberDaemon(info: DaemonInfo): void {
  try {
    localStorage.setItem("layer8.daemon", JSON.stringify(info));
  } catch {
    // Private mode: the user pastes the token again next time.
  }
}

export class Api {
  constructor(readonly info: DaemonInfo) {}

  private async req<T>(method: string, path: string, body?: unknown): Promise<T> {
    const res = await fetch(this.info.url + path, {
      method,
      headers: {
        Authorization: `Bearer ${this.info.token}`,
        ...(body === undefined ? {} : { "Content-Type": "application/json" }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
    const data = await res.json().catch(() => ({}));
    if (!res.ok) throw new Error(data.error ?? `HTTP ${res.status}`);
    return data as T;
  }

  state = () => this.req<AppState>("GET", "/api/state");
  config = () => this.req<Config>("GET", "/api/config");
  saveConfig = (c: Partial<Config>) => this.req("PUT", "/api/config", c);

  addWorkspace = (path: string) => this.req<Workspace>("POST", "/api/workspaces", { path });
  removeWorkspace = (id: string) => this.req("DELETE", `/api/workspaces/${id}`);

  linearIssues = (q = "") =>
    this.req<LinearIssueSummary[]>("GET", `/api/linear/issues${q ? `?q=${encodeURIComponent(q)}` : ""}`);

  createTask = (body: { workspaceId: string; linearIssue?: string; title?: string }) =>
    this.req<Task>("POST", "/api/tasks", body);
  archiveTask = (id: string, removeWorktree: boolean) =>
    this.req("POST", `/api/tasks/${id}/archive`, { removeWorktree });
  open = (taskId: string, target: "terminal" | "editor") => this.req("POST", `/api/tasks/${taskId}/open`, { target });
  artifacts = (taskId: string) => this.req<Artifact[]>("GET", `/api/tasks/${taskId}/artifacts`);
  artifact = (taskId: string, name: string) =>
    this.req<{ content: string }>("GET", `/api/tasks/${taskId}/artifacts/${encodeURIComponent(name)}`);

  startSession = (taskId: string, kind: SessionKind, prompt = "") =>
    this.req<Session>("POST", `/api/tasks/${taskId}/sessions`, { kind, prompt });
  events = (sessionId: string) => this.req<SessionEvent[]>("GET", `/api/sessions/${sessionId}/events`);
  send = (sessionId: string, text: string) => this.req("POST", `/api/sessions/${sessionId}/messages`, { text });
  interrupt = (sessionId: string) => this.req("POST", `/api/sessions/${sessionId}/interrupt`);
  resumeInTerminal = (sessionId: string) => this.req("POST", `/api/sessions/${sessionId}/open-terminal`);
  decide = (approvalId: string, allow: boolean, message?: string) =>
    this.req("POST", `/api/approvals/${approvalId}`, { allow, message });

  /** Reconnects with backoff; the daemon may restart while the app stays open. */
  subscribe(onPush: (p: ServerPush) => void, onStatus: (up: boolean) => void): () => void {
    let ws: WebSocket | null = null;
    let closed = false;
    let delay = 500;
    const connect = () => {
      ws = new WebSocket(`${this.info.url.replace(/^http/, "ws")}/ws?token=${this.info.token}`);
      ws.onopen = () => {
        delay = 500;
        onStatus(true);
      };
      ws.onmessage = (m) => onPush(JSON.parse(m.data));
      ws.onclose = () => {
        onStatus(false);
        if (!closed) setTimeout(connect, (delay = Math.min(delay * 2, 8000)));
      };
    };
    connect();
    return () => {
      closed = true;
      ws?.close();
    };
  }
}
