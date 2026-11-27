/** A node address such as `app`, `project:api-gateway` or `task:eng-482/pr2`. */
export type Addr = string;

export interface Summary {
  addr: Addr;
  kind: string;
  title: string;
  status?: string;
  children?: Summary[];
}

export interface Action {
  name: string;
  help: string;
  args?: unknown;
}

export interface Node {
  addr: Addr;
  kind: string;
  title: string;
  fields?: Record<string, unknown>;
  children: Summary[];
  actions: Action[];
}

export interface Op {
  target: Addr;
  action: string;
  args?: unknown;
}

export interface Result {
  ok: boolean;
  addr: Addr;
  message?: string;
  data?: unknown;
}

export type UiCommand =
  | { verb: "open-panel"; panel: string; position?: string }
  | { verb: "focus"; addr: Addr }
  | { verb: "preset"; name: string; layout?: unknown }
  | { verb: string; [k: string]: unknown };

/** One session transcript entry as `/api/sessions/{id8}/events` and the `session` SSE event carry it. Partial events have no id. */
export interface SessionEvent {
  id?: number;
  type: "user" | "delivered" | "text" | "thinking" | "tool_use" | "tool_result" | "approval" | "result" | "error" | "system";
  at?: string;
  partial?: boolean;
  data: Record<string, any>;
}

/** One message from the daemon's `/api/events` stream. */
export type L8Event =
  | { type: "node"; data: { addrs: Addr[] } }
  | { type: "session"; data: { session: string; events: SessionEvent[] } }
  | { type: "ui"; data: UiCommand }
  | { type: "toast"; data: { level: string; text: string } };

export const eventTypes = ["node", "session", "ui", "toast"] as const;

/** How many levels of children a read includes: none, direct, or grandchildren too. */
export type Depth = 0 | 1 | 2;

/** The UI's view of the daemon: read nodes, run actions, follow events. */
export interface Client {
  mode: "daemon" | "mock";
  label: string;
  getNode(addr: Addr, depth?: Depth): Promise<Node>;
  /** Runs ops as actor: `user` by default, or `plugin:<name>`. */
  doOps(ops: Op[], actor?: string): Promise<Result[]>;
  /** Reads a JSON endpoint of the daemon such as `/api/diff?task=…`. */
  getJSON<T>(path: string): Promise<T>;
  subscribe(listener: (e: L8Event) => void): () => void;
  /** Fetches raw content from a daemon path such as `/api/artifact?addr=…`. */
  raw(path: string): Promise<Blob>;
}
