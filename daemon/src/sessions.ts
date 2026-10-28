import { query, type CanUseTool, type Options, type SDKMessage } from "@anthropic-ai/claude-agent-sdk";
import { bus } from "./bus.js";
import type { Store } from "./db.js";
import { taskContext } from "./prompts.js";
import type { Session, SessionEventType } from "./types.js";

/** Lets tests replace the SDK without spawning Claude Code. */
export type QueryFn = (params: { prompt: string; options: Options }) => AsyncIterable<SDKMessage> & {
  interrupt?: () => Promise<void>;
};

type Running = { abort: AbortController; interrupt?: () => Promise<void>; stopped?: boolean };
type PendingApproval = { sessionId: string; resolve: (allow: boolean, message?: string) => void };

const TOOL_RESULT_LIMIT = 20_000;

export class SessionManager {
  private running = new Map<string, Running>();
  private pendingPrompt = new Map<string, string>();
  private approvals = new Map<string, PendingApproval>();

  constructor(
    private readonly store: Store,
    private readonly maxConcurrent: () => number,
    private readonly queryFn: QueryFn = query as unknown as QueryFn,
  ) {}

  /** Creates a session and queues its first prompt; it starts as soon as a slot is free. */
  create(taskId: string, kind: Session["kind"], title: string, prompt: string): Session {
    const task = this.store.getTask(taskId);
    if (!task) throw new Error(`no task ${taskId}`);
    const session = this.store.insertSession({ taskId, kind, title, cwd: task.worktreePath });
    this.enqueue(session.id, prompt);
    return session;
  }

  /** Follow-up message: resumes the Claude session with a new turn. */
  send(sessionId: string, text: string): void {
    const s = this.store.getSession(sessionId);
    if (!s) throw new Error(`no session ${sessionId}`);
    if (this.running.has(sessionId) || s.status === "queued") throw new Error("session is busy");
    this.enqueue(sessionId, text);
  }

  async interrupt(sessionId: string): Promise<void> {
    const r = this.running.get(sessionId);
    if (r) {
      r.stopped = true;
      for (const a of [...this.approvals.values()]) {
        if (a.sessionId === sessionId) a.resolve(false, "Interrupted by user");
      }
      if (r.interrupt) await r.interrupt().catch(() => r.abort.abort());
      else r.abort.abort();
      return;
    }
    if (this.pendingPrompt.delete(sessionId)) {
      this.store.setSessionStatus(sessionId, "interrupted");
      bus.emit("changed");
    }
  }

  resolveApproval(approvalId: string, allow: boolean, message?: string): void {
    const a = this.approvals.get(approvalId);
    if (!a) throw new Error("approval is no longer pending");
    a.resolve(allow, message);
  }

  /** Called at startup: the previous daemon's work can be resumed but not continued. */
  recover(): void {
    this.store.markOrphanedSessions();
    for (const s of this.store.queuedSessions()) this.store.setSessionStatus(s.id, "interrupted");
  }

  private enqueue(sessionId: string, prompt: string): void {
    this.pendingPrompt.set(sessionId, prompt);
    this.store.setSessionStatus(sessionId, "queued");
    bus.emit("changed");
    this.pump();
  }

  private pump(): void {
    for (const s of this.store.queuedSessions()) {
      if (this.running.size >= this.maxConcurrent()) return;
      const prompt = this.pendingPrompt.get(s.id);
      if (prompt === undefined) continue;
      this.pendingPrompt.delete(s.id);
      void this.run(s, prompt);
    }
  }

  private emit(sessionId: string, type: SessionEventType, data: Record<string, unknown>): void {
    bus.emit("event", this.store.addEvent(sessionId, type, data));
  }

  private async run(session: Session, prompt: string): Promise<void> {
    const task = this.store.getTask(session.taskId)!;
    const abort = new AbortController();
    const entry: Running = { abort };
    this.running.set(session.id, entry);
    this.store.setSessionStatus(session.id, "running");
    this.emit(session.id, "user", { text: prompt });
    bus.emit("changed");

    const canUseTool: CanUseTool = (toolName, input, { signal }) =>
      new Promise((resolve) => {
        const approval = this.store.insertApproval(session.id, toolName, input);
        this.store.setSessionStatus(session.id, "waiting_approval");
        bus.emit("changed");
        const settle = (allow: boolean, message?: string) => {
          if (!this.approvals.delete(approval.id)) return;
          this.store.setApprovalStatus(approval.id, allow ? "allowed" : "denied");
          this.store.setSessionStatus(session.id, "running");
          bus.emit("changed");
          resolve(
            allow
              ? { behavior: "allow", updatedInput: input }
              : { behavior: "deny", message: message || "The user denied this action." },
          );
        };
        this.approvals.set(approval.id, { sessionId: session.id, resolve: settle });
        signal.addEventListener("abort", () => settle(false, "Aborted"), { once: true });
      });

    let failed = false;
    try {
      const q = this.queryFn({
        prompt,
        options: {
          cwd: task.worktreePath,
          additionalDirectories: [task.artifactsPath],
          resume: this.store.getSession(session.id)?.claudeSessionId ?? undefined,
          abortController: abort,
          canUseTool,
          // The user's own settings, CLAUDE.md, skills and permission rules apply.
          settingSources: ["user", "project", "local"],
          systemPrompt: { type: "preset", preset: "claude_code", append: taskContext(task) },
        },
      });
      entry.interrupt = q.interrupt?.bind(q);
      for await (const msg of q) this.handle(session.id, msg);
    } catch (err) {
      failed = !abort.signal.aborted && !entry.stopped;
      this.emit(session.id, "error", { message: err instanceof Error ? err.message : String(err) });
    } finally {
      this.running.delete(session.id);
      const status = abort.signal.aborted || entry.stopped ? "interrupted" : failed ? "failed" : "idle";
      // A result message may already have set failed; don't overwrite it with idle.
      const current = this.store.getSession(session.id)?.status;
      if (!(status === "idle" && current === "failed")) this.store.setSessionStatus(session.id, status);
      bus.emit("changed");
      this.pump();
    }
  }

  private handle(sessionId: string, msg: SDKMessage): void {
    switch (msg.type) {
      case "system":
        if ("subtype" in msg && msg.subtype === "init") {
          this.store.setClaudeSessionId(sessionId, msg.session_id);
          bus.emit("changed");
        }
        return;
      case "assistant": {
        // Subagent traffic is tagged with its parent tool call; keep the main thread readable.
        const parent = msg.parent_tool_use_id ?? null;
        for (const block of msg.message.content as any[]) {
          if (block.type === "text" && block.text.trim()) this.emit(sessionId, "text", { text: block.text, parent });
          else if (block.type === "tool_use")
            this.emit(sessionId, "tool_use", { id: block.id, name: block.name, input: block.input, parent });
        }
        return;
      }
      case "user": {
        const content = msg.message.content;
        if (typeof content === "string") return;
        for (const block of content as any[]) {
          if (block.type !== "tool_result") continue;
          let text =
            typeof block.content === "string"
              ? block.content
              : (block.content ?? []).map((c: any) => (c.type === "text" ? c.text : `[${c.type}]`)).join("\n");
          if (text.length > TOOL_RESULT_LIMIT) text = text.slice(0, TOOL_RESULT_LIMIT) + "\n… (truncated)";
          this.emit(sessionId, "tool_result", { toolUseId: block.tool_use_id, isError: !!block.is_error, text });
        }
        return;
      }
      case "result": {
        this.store.addSessionCost(sessionId, msg.total_cost_usd ?? 0);
        const ok = msg.subtype === "success" && !msg.is_error;
        this.emit(sessionId, "result", {
          ok,
          subtype: msg.subtype,
          durationMs: msg.duration_ms,
          costUsd: msg.total_cost_usd,
          turns: msg.num_turns,
        });
        if (!ok) this.store.setSessionStatus(sessionId, "failed");
        return;
      }
      default:
        return;
    }
  }
}
