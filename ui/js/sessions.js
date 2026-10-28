import { html, useState, useEffect, useRef, useMemo } from "/vendor/preact-htm.js";
import { get, post, onSessionEvents } from "./api.js";
import { clock, cls, usd } from "./util.js";

export function SessionsPane({ state, task, slice, sessions, session, onSelect, run, onStart }) {
  const approvals = state.approvals.filter((a) => session && a.sessionId === session.id);
  if (!sessions.length) {
    return html`<div class="pane">
      <div class="log"><div class="empty">
        <div class="display">Ready</div>
        ${slice
          ? html`Press <kbd>I</kbd> to implement <b>${slice.key}</b> in a fresh session. It reads the plan section and the handoff from the PR it is stacked on.`
          : html`Start with <kbd>R</kbd> research, then <kbd>P</kbd> plan. Each workflow runs in a fresh context and hands off through the docs. Or type below for a free-form session.`}
      </div></div>
      <${Composer} busy=${false} placeholder="Start a chat session here" onSend=${(t) => onStart("chat", t)} />
    </div>`;
  }
  const busy = session && ["queued", "running", "waiting_approval"].includes(session.status);
  return html`<div class="pane">
    <div class="tabs subtabs">
      ${sessions.map((s) => html`<button key=${s.id} class="tab" aria-selected=${session && s.id === session.id} onClick=${() => onSelect(s.id)} title=${s.title}>
        <span class=${cls("sq", s.status)}></span>${s.title.length > 26 ? s.title.slice(0, 26) + "…" : s.title}
      </button>`)}
      <button class="tab" title="New chat session here" onClick=${() => onSelect("__new")}>+</button>
    </div>
    ${session
      ? html`
          <${Log} key=${session.id} session=${session}>
            ${approvals.map((a) => html`<${Approval} key=${a.id} approval=${a} run=${run} />`)}
          </${Log}>
          <${Composer}
            busy=${busy}
            busyText=${session.status === "queued" ? "Queued: waiting for a free agent slot" : "Agent is working…"}
            placeholder=${`Message this ${session.kind} session`}
            onSend=${(t) => run(post(`/api/sessions/${session.id}/messages`, { text: t }))}
            onStop=${() => run(post(`/api/sessions/${session.id}/interrupt`))}
          />`
      : html`<div class="log"><div class="empty">New free-form session in this ${slice ? "PR's" : "task's"} worktree.</div></div>
          <${Composer} busy=${false} placeholder="Start a chat session here" onSend=${(t) => onStart("chat", t)} />`}
  </div>`;
}

/** The argument worth showing on a collapsed tool line. */
function headline(name, input = {}) {
  if (name === "Bash") return input.command || "";
  if (input.file_path) return input.file_path;
  if (input.pattern) return `${input.pattern}${input.path ? "  in " + input.path : ""}`;
  if (input.url) return input.url;
  if (input.description) return input.description;
  if (input.prompt) return input.prompt;
  if (name === "TodoWrite") return `${(input.todos || []).length} items`;
  return JSON.stringify(input);
}

function Log({ session, children }) {
  const [events, setEvents] = useState([]);
  const box = useRef(null);
  const pinned = useRef(true);
  const lastId = useRef(0);

  useEffect(() => {
    let live = true;
    const buffered = [];
    let loaded = false;
    const add = (evs) => {
      const mine = evs.filter((e) => e.sessionId === session.id && e.id > lastId.current);
      if (!mine.length) return;
      lastId.current = mine[mine.length - 1].id;
      setEvents((prev) => [...prev, ...mine]);
    };
    const off = onSessionEvents((evs) => (loaded ? add(evs) : buffered.push(...evs)));
    get(`/api/sessions/${session.id}/events`).then((hist) => {
      if (!live) return;
      lastId.current = hist.length ? hist[hist.length - 1].id : 0;
      setEvents(hist);
      loaded = true;
      add(buffered);
    });
    return () => {
      live = false;
      off();
    };
  }, [session.id]);

  // Catch up if pushes were missed (e.g. the window was hidden).
  useEffect(() => {
    if (!lastId.current) return;
    get(`/api/sessions/${session.id}/events?after=${lastId.current}`).then((more) => {
      const fresh = more.filter((e) => e.id > lastId.current);
      if (fresh.length) {
        lastId.current = fresh[fresh.length - 1].id;
        setEvents((prev) => [...prev, ...fresh]);
      }
    });
  }, [session.status]);

  const results = useMemo(() => {
    const m = new Map();
    for (const e of events) if (e.type === "tool_result") m.set(e.data.toolUseId, e);
    return m;
  }, [events]);

  useEffect(() => {
    const el = box.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [events, children]);

  return html`<div class="log selectable" ref=${box} onScroll=${(e) => {
    const el = e.currentTarget;
    pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  }}>
    ${events.map((e) => html`<${Line} key=${e.id} e=${e} result=${e.type === "tool_use" ? results.get(e.data.id) : undefined} />`)}
    ${session.status === "failed" && session.lastError && !events.some((e) => e.type === "error") &&
    html`<div class="line error"><span class="t"></span><span class="g">✕</span><div class="body">${session.lastError}</div></div>`}
    ${children}
  </div>`;
}

function Line({ e, result }) {
  const [open, setOpen] = useState(false);
  const sub = e.data.parent ? " sub" : "";
  const t = html`<span class="t">${clock(e.at)}</span>`;
  switch (e.type) {
    case "user":
      return html`<div class="line user">${t}<span class="g"></span><div class="body">${e.data.text}</div></div>`;
    case "text":
      return html`<div class=${"line" + sub}>${t}<span class="g"></span><div class="body md" dangerouslySetInnerHTML=${{ __html: e.data.html || "" }}></div></div>`;
    case "tool_use": {
      const err = result && result.data.isError;
      return html`<div class=${"line" + sub}>${t}<span class="g">${open ? "▾" : "▸"}</span>
        <div class="body">
          <div class="tool" onClick=${() => setOpen(!open)}>
            <span class="name">${e.data.name}</span>
            <span class="arg">${headline(e.data.name, e.data.input)}</span>
            <span class=${cls("st", err && "err")}>${result ? (err ? "error" : "ok") : "…"}</span>
          </div>
          ${open && html`<div class="detail">${JSON.stringify(e.data.input, null, 2)}${result ? "\n\n──── result ────\n" + result.data.text : ""}</div>`}
        </div>
      </div>`;
    }
    case "result": {
      const ok = e.data.ok;
      const label = e.data.interrupted ? "stopped" : ok ? "done" : `ended: ${e.data.subtype}`;
      return html`<div class=${cls("line result", !ok && !e.data.interrupted && "badr")}>${t}<span class="g">${ok ? "✓" : "✕"}</span>
        <div class="body">${label} · ${Math.round((e.data.durationMs || 0) / 1000)}s · ${e.data.turns || 0} turns · session ${usd(e.data.costUsd)}</div></div>`;
    }
    case "error":
      return html`<div class="line error">${t}<span class="g">✕</span><div class="body">${e.data.message}</div></div>`;
    default:
      return null;
  }
}

function describe(a) {
  const i = a.input || {};
  if (a.toolName === "Bash") return i.command || "";
  if (a.toolName === "Edit" || a.toolName === "Write" || a.toolName === "MultiEdit") {
    const body = i.new_string || i.content || "";
    return `${i.file_path || ""}\n\n${body.length > 1500 ? body.slice(0, 1500) + "\n…" : body}`;
  }
  if (typeof i.file_path === "string") return i.file_path;
  return JSON.stringify(i, null, 2);
}

function Approval({ approval, run }) {
  const [reason, setReason] = useState("");
  const decide = (allow) => run(post(`/api/approvals/${approval.id}`, { allow, message: allow ? "" : reason }));
  return html`<div class="approval"><div>
    <div class="what">Approve ${approval.toolName}?</div>
    <pre class="selectable">${describe(approval)}</pre>
    <div class="row">
      <button class="key primary" onClick=${() => decide(true)}><kbd>A</kbd> Allow</button>
      <input class="field" placeholder="Or tell the agent what to do instead…" value=${reason}
        onInput=${(e) => setReason(e.target.value)} onKeyDown=${(e) => e.key === "Enter" && decide(false)} />
      <button class="key danger" onClick=${() => decide(false)}><kbd>D</kbd> Deny</button>
    </div>
  </div></div>`;
}

export function Composer({ busy, busyText, placeholder, onSend, onStop }) {
  const [text, setText] = useState("");
  const ref = useRef(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [text]);
  const send = () => {
    if (!text.trim() || busy) return;
    onSend(text.trim());
    setText("");
  };
  return html`<div class="composer">
    <span class="prompt">›</span>
    <textarea ref=${ref} rows="1" value=${text} placeholder=${busy ? busyText : placeholder}
      onInput=${(e) => setText(e.target.value)}
      onKeyDown=${(e) => {
        if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
          e.preventDefault();
          send();
        }
        if (e.key === "Escape") e.target.blur();
      }}></textarea>
    <div class="send">
      ${busy && onStop
        ? html`<button class="key danger" onClick=${onStop}>■ Stop</button>`
        : html`<button class="key" disabled=${!text.trim()} onClick=${send}>↵ Send</button>`}
    </div>
  </div>`;
}
