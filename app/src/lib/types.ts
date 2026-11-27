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
  | { verb: "open-panel"; panel: string }
  | { verb: "focus"; addr: Addr }
  | { verb: string; [k: string]: unknown };

/** One message from the daemon's `/api/events` stream. */
export type L8Event =
  | { type: "node"; data: { addrs: Addr[] } }
  | { type: "session"; data: { session: string; events: unknown[] } }
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
  doOps(ops: Op[]): Promise<Result[]>;
  subscribe(listener: (e: L8Event) => void): () => void;
}
