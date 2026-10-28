// Shared API contract between layer8d and the app. Type-only: the app imports
// this file directly, so keep it free of runtime code.

export type Workspace = {
  id: string;
  name: string;
  path: string;
  isGit: boolean;
  defaultBranch: string | null;
  createdAt: string;
};

export type LinearRef = {
  id: string;
  identifier: string;
  url: string;
  branchName: string;
};

export type TaskStatus = "active" | "archived";

export type Task = {
  id: string;
  workspaceId: string;
  slug: string;
  title: string;
  linear: LinearRef | null;
  branch: string | null;
  baseRef: string | null;
  worktreePath: string;
  artifactsPath: string;
  tmuxSession: string | null;
  status: TaskStatus;
  createdAt: string;
};

export type SessionKind = "research" | "plan" | "implement" | "chat";

export type SessionStatus =
  | "queued"
  | "running"
  | "waiting_approval"
  | "idle"
  | "failed"
  | "interrupted";

export type Session = {
  id: string;
  taskId: string;
  kind: SessionKind;
  title: string;
  claudeSessionId: string | null;
  status: SessionStatus;
  cwd: string;
  costUsd: number;
  createdAt: string;
  updatedAt: string;
};

export type SessionEventType =
  | "user"
  | "text"
  | "tool_use"
  | "tool_result"
  | "result"
  | "error"
  | "status";

export type SessionEvent = {
  id: number;
  sessionId: string;
  type: SessionEventType;
  at: string;
  data: Record<string, unknown>;
};

export type ApprovalStatus = "pending" | "allowed" | "denied";

export type Approval = {
  id: string;
  sessionId: string;
  toolName: string;
  input: Record<string, unknown>;
  status: ApprovalStatus;
  createdAt: string;
};

export type LinearIssueSummary = {
  id: string;
  identifier: string;
  title: string;
  url: string;
  branchName: string;
  priority: number;
  state: { name: string; type: string; color: string };
  team: string;
  updatedAt: string;
};

export type TerminalApp = "ghostty" | "wezterm" | "kitty" | "iterm" | "terminal" | "custom";
export type EditorApp = "code" | "cursor" | "zed" | "idea" | "custom";

export type Config = {
  port: number;
  linearApiKey: string | null;
  terminal: TerminalApp;
  /** For terminal "custom": template with {cwd} and {cmd}. */
  terminalCommand: string | null;
  editor: EditorApp;
  /** For editor "custom": template with {path}. */
  editorCommand: string | null;
  useTmux: boolean;
  maxConcurrentSessions: number;
  worktreeRoot: string;
};

export type AppState = {
  workspaces: Workspace[];
  tasks: Task[];
  sessions: Session[];
  approvals: Approval[];
};

export type Artifact = { name: string; size: number; updatedAt: string };

/** Messages pushed over the /ws socket. */
export type ServerPush =
  | { type: "state"; state: AppState }
  | { type: "event"; event: SessionEvent };
