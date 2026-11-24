import { html, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post } from "./api.js";
import { useEvents } from "./events.js";
import { Composer, PermissionChip } from "./composer.js";

const statusLabel = {
  starting: "starting",
  running: "running",
  waiting_approval: "needs approval",
  ready_for_input: "ready",
  stopped: "stopped",
  failed: "failed",
};

export const StatusDot = ({ status }) => html`<span class=${`dot s-${status}`} title=${statusLabel[status] || status}></span>`;

const money = (usd) => `$${(usd || 0).toFixed(2)}`;
const kTokens = (n) => (n >= 1000 ? `${Math.round(n / 1000)}k` : `${n || 0}`);

/** useConversation keeps a session's events, approvals and streaming draft in sync with the daemon. */
function useConversation(sessionId) {
  const [events, setEvents] = useState([]);
  const [approvals, setApprovals] = useState({});
  const [draft, setDraft] = useState("");
  const last = useRef(0);
  const loading = useRef(null);

  const loadEvents = useCallback(async () => {
    if (loading.current) return loading.current;
    loading.current = (async () => {
      try {
        const fresh = await get(`/api/sessions/${sessionId}/events?after=${last.current}`);
        if (fresh.length) {
          last.current = fresh[fresh.length - 1].seq;
          setEvents((old) => [...old, ...fresh]);
          if (fresh.some((e) => (e.kind === "text" && !e.parentToolUseId) || e.kind === "result")) setDraft("");
        }
      } finally {
        loading.current = null;
      }
    })();
    return loading.current;
  }, [sessionId]);

  const loadApprovals = useCallback(async () => {
    const list = await get(`/api/sessions/${sessionId}/approvals`);
    setApprovals(Object.fromEntries(list.map((a) => [a.id, a.status])));
  }, [sessionId]);

  useEffect(() => {
    last.current = 0;
    setEvents([]);
    setDraft("");
    loadEvents();
    loadApprovals();
  }, [sessionId]);

  useEvents((e) => {
    if (e.kind === "open") return (loadEvents(), loadApprovals());
    if (e.kind === "event" && e.id === sessionId) return loadEvents().then(() => e.seq > last.current && loadEvents());
    if (e.kind === "delta" && e.id === sessionId) return setDraft(e.text);
    if (e.kind === "approval" && e.sessionId === sessionId) return loadApprovals();
  });
  return { events, approvals, draft };
}

function Text({ body }) {
  return html`<div class="msg text">${body.text}</div>`;
}

function UserMessage({ body }) {
  return html`<div class="msg user">${body.text}</div>`;
}

/** ToolCard is a collapsible tool call; subagent rows nest inside it and its result shows when expanded. */
function ToolCard({ event, rows, approvals }) {
  const [open, setOpen] = useState(false);
  const tool = event.body;
  const result = rows.find((r) => r.kind === "tool_result" && r.body.toolUseId === tool.id);
  const children = rows.filter((r) => r.parentToolUseId === tool.id && r.kind !== "tool_result");
  const state = !result ? "pending" : result.body.isError ? "error" : "done";
  return html`<div class=${`tool ${state}`}>
    <div class="tool-head" onClick=${() => setOpen(!open)}>
      <span class="caret">${open ? "▾" : "▸"}</span>
      <span class="mono">${tool.summary || tool.name}</span>
      ${children.length > 0 && html`<span class="tag">${children.length} steps</span>`}
    </div>
    ${children.length > 0 &&
    html`<div class="nested">${children.map((c) => html`<${Row} key=${c.seq} event=${c} rows=${rows} approvals=${approvals} />`)}</div>`}
    ${open &&
    html`<div class="tool-body">
      <pre class="mono">${JSON.stringify(tool.input, null, 2)}</pre>
      ${result && html`<pre class=${`mono ${result.body.isError ? "err" : ""}`}>${result.body.preview || "(no output)"}</pre>`}
    </div>`}
  </div>`;
}

/** ApprovalCard asks the user to allow, always allow or deny a tool call. */
function ApprovalCard({ event, status }) {
  const [error, setError] = useState("");
  const { approvalId, tool, input } = event.body;
  const decide = (decision) => post(`/api/approvals/${approvalId}`, { decision }).catch((err) => setError(err.message));
  const detail = input && (input.command || input.file_path || input.url || input.pattern);
  return html`<div class=${`approval ${status || "pending"}`}>
    <div class="lbl">${status === "pending" || !status ? "Approval needed" : status}</div>
    <div class="mono">${tool}${detail ? `: ${detail}` : ""}</div>
    ${!detail && html`<pre class="mono">${JSON.stringify(input, null, 2)}</pre>`}
    ${(status === "pending" || !status) &&
    html`<div class="btns">
      <button class="primary" onClick=${() => decide("allow")}>Allow</button>
      <button onClick=${() => decide("always")}>Always allow</button>
      <button onClick=${() => decide("deny")}>Deny</button>
      ${error && html`<span class="err">${error}</span>`}
    </div>`}
  </div>`;
}

function Row({ event, rows, approvals }) {
  switch (event.kind) {
    case "user":
      return html`<${UserMessage} body=${event.body} />`;
    case "text":
      return html`<${Text} body=${event.body} />`;
    case "tool":
      return html`<${ToolCard} event=${event} rows=${rows} approvals=${approvals} />`;
    case "approval":
      return html`<${ApprovalCard} event=${event} status=${approvals[event.body.approvalId]} />`;
    case "result":
      return event.body.isError
        ? html`<div class="msg system err">${event.body.text || "The turn failed."}</div>`
        : html`<div class="msg result">${money(event.body.costUsd)} · ${(event.body.durationMs / 1000).toFixed(1)}s</div>`;
    case "system":
      return html`<div class="msg system">${event.body.text}</div>`;
    default:
      return null;
  }
}

/** Conversation renders a session's events top to bottom and follows new ones while scrolled to the end. */
function Conversation({ sessionId }) {
  const { events, approvals, draft } = useConversation(sessionId);
  const scroller = useRef(null);
  const pinned = useRef(true);
  useEffect(() => {
    const el = scroller.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [events, draft, approvals]);
  const onScroll = (e) => {
    const el = e.currentTarget;
    pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  };
  const top = events.filter((e) => !e.parentToolUseId && e.kind !== "tool_result");
  return html`<div class="conversation" ref=${scroller} onScroll=${onScroll}>
    ${top.map((e) => html`<${Row} key=${e.seq} event=${e} rows=${events} approvals=${approvals} />`)}
    ${draft && html`<div class="msg text streaming">${draft}</div>`}
    ${!events.length && html`<div class="dim">No messages yet.</div>`}
  </div>`;
}

/** SessionView is one session: its header, conversation and composer. */
export function SessionView({ session }) {
  const [error, setError] = useState("");
  const act = (path) => post(`/api/sessions/${session.id}/${path}`).catch((err) => setError(err.message));
  const busy = session.inTurn;
  return html`<div class="session">
    <div class="shead">
      <${StatusDot} status=${session.status} />
      <b>${session.label}</b>
      <span class="dim">${statusLabel[session.status] || session.status}</span>
      ${session.model && html`<span class="tag">${session.model}</span>`}
      <span class="spacer"></span>
      ${error && html`<span class="err">${error}</span>`}
      <span class="mono dim" title="context tokens">${kTokens(session.contextTokens)} ctx</span>
      <span class="mono" title="cost">${money(session.costUsd)}</span>
      ${!["stopped", "failed"].includes(session.status) && html`<button onClick=${() => act("stop")}>Stop</button>`}
    </div>
    <${Conversation} sessionId=${session.id} />
    <${Composer}
      autofocus
      queued=${session.queued}
      busy=${busy}
      onInterrupt=${() => act("interrupt")}
      onSend=${(text) => post(`/api/sessions/${session.id}/messages`, { text })}
      chip=${html`<${PermissionChip} mode=${session.permissionMode} />`}
    />
  </div>`;
}

/** NewSession starts a session in a task from a label and a first message. */
export function NewSession({ task, onStarted }) {
  const [label, setLabel] = useState("");
  const [mode, setMode] = useState("default");
  return html`<div class="session">
    <div class="shead">
      <span class="lbl">New session</span>
      <input class="label-input" placeholder="label, e.g. research" value=${label} onInput=${(e) => setLabel(e.target.value)} />
    </div>
    <div class="conversation"><div class="dim">The session runs claude in ${task.worktree} with your full Claude Code config.</div></div>
    <${Composer}
      autofocus
      placeholder="First message…"
      onSend=${async (prompt) => onStarted(await post(`/api/tasks/${task.id}/sessions`, { label, prompt, permissionMode: mode }))}
      chip=${html`<${PermissionChip} mode=${mode} onChange=${setMode} />`}
    />
  </div>`;
}
