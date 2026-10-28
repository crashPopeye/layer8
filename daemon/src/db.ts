import { DatabaseSync } from "node:sqlite";
import { randomUUID } from "node:crypto";
import type {
  Approval,
  ApprovalStatus,
  Session,
  SessionEvent,
  SessionEventType,
  SessionStatus,
  Task,
  Workspace,
} from "./types.js";

const schema = `
CREATE TABLE IF NOT EXISTS workspaces (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  path TEXT NOT NULL UNIQUE,
  is_git INTEGER NOT NULL,
  default_branch TEXT,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS tasks (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  slug TEXT NOT NULL UNIQUE,
  title TEXT NOT NULL,
  linear TEXT,
  branch TEXT,
  base_ref TEXT,
  worktree_path TEXT NOT NULL,
  artifacts_path TEXT NOT NULL,
  tmux_session TEXT,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  title TEXT NOT NULL,
  claude_session_id TEXT,
  status TEXT NOT NULL,
  cwd TEXT NOT NULL,
  cost_usd REAL NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS session_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  data TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS session_events_by_session ON session_events(session_id, id);
CREATE TABLE IF NOT EXISTS approvals (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  tool_name TEXT NOT NULL,
  input TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL
);
`;

const now = () => new Date().toISOString();

type Row = Record<string, any>;

const toWorkspace = (r: Row): Workspace => ({
  id: r.id,
  name: r.name,
  path: r.path,
  isGit: !!r.is_git,
  defaultBranch: r.default_branch,
  createdAt: r.created_at,
});

const toTask = (r: Row): Task => ({
  id: r.id,
  workspaceId: r.workspace_id,
  slug: r.slug,
  title: r.title,
  linear: r.linear ? JSON.parse(r.linear) : null,
  branch: r.branch,
  baseRef: r.base_ref,
  worktreePath: r.worktree_path,
  artifactsPath: r.artifacts_path,
  tmuxSession: r.tmux_session,
  status: r.status,
  createdAt: r.created_at,
});

const toSession = (r: Row): Session => ({
  id: r.id,
  taskId: r.task_id,
  kind: r.kind,
  title: r.title,
  claudeSessionId: r.claude_session_id,
  status: r.status,
  cwd: r.cwd,
  costUsd: r.cost_usd,
  createdAt: r.created_at,
  updatedAt: r.updated_at,
});

const toEvent = (r: Row): SessionEvent => ({
  id: r.id,
  sessionId: r.session_id,
  type: r.type,
  at: r.at,
  data: JSON.parse(r.data),
});

const toApproval = (r: Row): Approval => ({
  id: r.id,
  sessionId: r.session_id,
  toolName: r.tool_name,
  input: JSON.parse(r.input),
  status: r.status,
  createdAt: r.created_at,
});

export class Store {
  readonly db: DatabaseSync;

  constructor(file: string) {
    this.db = new DatabaseSync(file);
    this.db.exec("PRAGMA journal_mode = WAL; PRAGMA foreign_keys = ON;");
    this.db.exec(schema);
  }

  // Workspaces

  listWorkspaces(): Workspace[] {
    return this.db.prepare("SELECT * FROM workspaces ORDER BY name").all().map(toWorkspace);
  }

  getWorkspace(id: string): Workspace | undefined {
    const r = this.db.prepare("SELECT * FROM workspaces WHERE id = ?").get(id);
    return r ? toWorkspace(r) : undefined;
  }

  insertWorkspace(w: Omit<Workspace, "id" | "createdAt">): Workspace {
    const ws: Workspace = { ...w, id: randomUUID(), createdAt: now() };
    this.db
      .prepare("INSERT INTO workspaces VALUES (?, ?, ?, ?, ?, ?)")
      .run(ws.id, ws.name, ws.path, ws.isGit ? 1 : 0, ws.defaultBranch, ws.createdAt);
    return ws;
  }

  deleteWorkspace(id: string): void {
    this.db.prepare("DELETE FROM workspaces WHERE id = ?").run(id);
  }

  // Tasks

  listTasks(): Task[] {
    return this.db.prepare("SELECT * FROM tasks ORDER BY created_at DESC").all().map(toTask);
  }

  getTask(id: string): Task | undefined {
    const r = this.db.prepare("SELECT * FROM tasks WHERE id = ?").get(id);
    return r ? toTask(r) : undefined;
  }

  slugTaken(slug: string): boolean {
    return !!this.db.prepare("SELECT 1 FROM tasks WHERE slug = ?").get(slug);
  }

  insertTask(t: Omit<Task, "createdAt">): Task {
    const task: Task = { ...t, createdAt: now() };
    this.db
      .prepare("INSERT INTO tasks VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
      .run(
        task.id,
        task.workspaceId,
        task.slug,
        task.title,
        task.linear ? JSON.stringify(task.linear) : null,
        task.branch,
        task.baseRef,
        task.worktreePath,
        task.artifactsPath,
        task.tmuxSession,
        task.status,
        task.createdAt,
      );
    return task;
  }

  setTaskStatus(id: string, status: Task["status"]): void {
    this.db.prepare("UPDATE tasks SET status = ? WHERE id = ?").run(status, id);
  }

  // Sessions

  listSessions(): Session[] {
    return this.db.prepare("SELECT * FROM sessions ORDER BY created_at").all().map(toSession);
  }

  getSession(id: string): Session | undefined {
    const r = this.db.prepare("SELECT * FROM sessions WHERE id = ?").get(id);
    return r ? toSession(r) : undefined;
  }

  insertSession(s: Pick<Session, "taskId" | "kind" | "title" | "cwd">): Session {
    const t = now();
    const session: Session = {
      ...s,
      id: randomUUID(),
      claudeSessionId: null,
      status: "queued",
      costUsd: 0,
      createdAt: t,
      updatedAt: t,
    };
    this.db
      .prepare("INSERT INTO sessions VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
      .run(
        session.id,
        session.taskId,
        session.kind,
        session.title,
        null,
        session.status,
        session.cwd,
        0,
        t,
        t,
      );
    return session;
  }

  setSessionStatus(id: string, status: SessionStatus): void {
    this.db.prepare("UPDATE sessions SET status = ?, updated_at = ? WHERE id = ?").run(status, now(), id);
  }

  setClaudeSessionId(id: string, claudeSessionId: string): void {
    this.db.prepare("UPDATE sessions SET claude_session_id = ? WHERE id = ?").run(claudeSessionId, id);
  }

  addSessionCost(id: string, usd: number): void {
    this.db.prepare("UPDATE sessions SET cost_usd = cost_usd + ? WHERE id = ?").run(usd, id);
  }

  /** Sessions that were mid-flight when the daemon stopped can be resumed, not continued. */
  markOrphanedSessions(): void {
    this.db
      .prepare(
        "UPDATE sessions SET status = 'interrupted', updated_at = ? WHERE status IN ('running', 'waiting_approval')",
      )
      .run(now());
    this.db.prepare("UPDATE approvals SET status = 'denied' WHERE status = 'pending'").run();
  }

  queuedSessions(): Session[] {
    return this.db
      .prepare("SELECT * FROM sessions WHERE status = 'queued' ORDER BY updated_at")
      .all()
      .map(toSession);
  }

  // Events

  addEvent(sessionId: string, type: SessionEventType, data: Record<string, unknown>): SessionEvent {
    const at = now();
    const res = this.db
      .prepare("INSERT INTO session_events (session_id, type, at, data) VALUES (?, ?, ?, ?)")
      .run(sessionId, type, at, JSON.stringify(data));
    return { id: Number(res.lastInsertRowid), sessionId, type, at, data };
  }

  listEvents(sessionId: string, afterId = 0): SessionEvent[] {
    return this.db
      .prepare("SELECT * FROM session_events WHERE session_id = ? AND id > ? ORDER BY id")
      .all(sessionId, afterId)
      .map(toEvent);
  }

  // Approvals

  insertApproval(sessionId: string, toolName: string, input: Record<string, unknown>): Approval {
    const a: Approval = {
      id: randomUUID(),
      sessionId,
      toolName,
      input,
      status: "pending",
      createdAt: now(),
    };
    this.db
      .prepare("INSERT INTO approvals VALUES (?, ?, ?, ?, ?, ?)")
      .run(a.id, sessionId, toolName, JSON.stringify(input), a.status, a.createdAt);
    return a;
  }

  setApprovalStatus(id: string, status: ApprovalStatus): void {
    this.db.prepare("UPDATE approvals SET status = ? WHERE id = ?").run(status, id);
  }

  pendingApprovals(): Approval[] {
    return this.db
      .prepare("SELECT * FROM approvals WHERE status = 'pending' ORDER BY created_at")
      .all()
      .map(toApproval);
  }
}
