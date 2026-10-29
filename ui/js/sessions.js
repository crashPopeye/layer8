import { html, useState, useEffect, useRef, useMemo, useCallback } from "/vendor/preact-htm.js";
import { get, post, put, del, upload, onSessionEvents } from "./api.js";
import { clock, cls, usd } from "./util.js";

export function SessionsPane({ state, task, slice, sessions, session, onSelect, run, onStart }) {
  const approvals = state.approvals.filter((a) => session && a.sessionId === session.id);
  const newComposer = html`<${Composer} task=${task} run=${run} placeholder="Start a chat session here" onSend=${(t, settings) => onStart("chat", t, settings)} />`;
  if (!sessions.length) {
    return html`<div class="pane">
      <div class="log"><div class="empty">
        <div class="display">Ready</div>
        ${slice
          ? html`Use <b>Implement</b> to implement <b>${slice.key}</b> in a fresh session. It reads the plan section and the handoff from the PR it is stacked on.`
          : html`Start with <b>Research</b>, then <b>Plan</b>. Each workflow runs in a fresh context and hands off through the docs. Or type below for a free-form session.`}
      </div></div>
      ${newComposer}
    </div>`;
  }
  const busy = session && ["queued", "running", "waiting_approval"].includes(session.status);
  const remove = async (s) => {
    const working = ["queued", "running", "waiting_approval"].includes(s.status);
    if (!confirm(working ? `Stop and delete "${s.title}"? Its history is removed.` : `Delete "${s.title}" and its history?`)) return;
    await run(del(`/api/sessions/${s.id}`));
    if (session && s.id === session.id) onSelect(null);
  };
  return html`<div class="pane">
    <div class="tabs subtabs">
      ${sessions.map((s) => html`<div key=${s.id} class="tab session-tab" aria-selected=${session && s.id === session.id} onClick=${() => onSelect(s.id)} title=${s.title}>
        <span class=${cls("sq", s.status)}></span>${s.title.length > 26 ? s.title.slice(0, 26) + "…" : s.title}
        <button class="x" title="Delete session" onClick=${(e) => { e.stopPropagation(); remove(s); }}>✕</button>
      </div>`)}
      <button class="tab" title="New chat session here" onClick=${() => onSelect("__new")}>+</button>
    </div>
    ${session
      ? html`
          <${Log} key=${session.id} session=${session}>
            ${approvals.map((a) => html`<${Approval} key=${a.id} approval=${a} run=${run} />`)}
          </${Log}>
          <${Composer}
            key=${session.id}
            task=${task}
            session=${session}
            run=${run}
            busy=${busy}
            busyText=${session.status === "queued" ? "Queued: waiting for a free agent slot" : "Agent is working…"}
            placeholder=${`Message this ${session.kind} session`}
            onSend=${(t) => run(post(`/api/sessions/${session.id}/messages`, { text: t }))}
            onStop=${() => run(post(`/api/sessions/${session.id}/interrupt`))}
          />`
      : html`<div class="log"><div class="empty">New free-form session in this ${slice ? "PR's" : "task's"} worktree.</div></div>
          ${newComposer}`}
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
      <button class="key primary" onClick=${() => decide(true)}>Allow</button>
      <input class="field" placeholder="Or tell the agent what to do instead…" value=${reason}
        onInput=${(e) => setReason(e.target.value)} onKeyDown=${(e) => e.key === "Enter" && decide(false)} />
      <button class="key danger" onClick=${() => decide(false)}>Deny</button>
    </div>
  </div></div>`;
}

export const HARNESSES = { claude: "Claude Code", codex: "Codex" };
const MODES = { default: "Ask", acceptEdits: "Auto-accept edits", auto: "Auto-accept", bypassPermissions: "Bypass permissions" };
const PREFS = "layer8.composer";

/** The settings new sessions start with: the last ones picked in a composer. */
export function sessionDefaults() {
  try {
    return { harness: "claude", model: "", effort: "", permissionMode: "acceptEdits", ...JSON.parse(localStorage.getItem(PREFS) || "{}") };
  } catch {
    return { harness: "claude", model: "", effort: "", permissionMode: "acceptEdits" };
  }
}

// The last list seen per task and harness, shown at once while a fresh one loads.
const capsCache = new Map();
function useCapabilities(taskId, harness) {
  const key = `${taskId}/${harness}`;
  const [caps, setCaps] = useState(capsCache.get(key) || null);
  const [tick, setTick] = useState(0);
  useEffect(() => {
    setCaps(capsCache.get(key) || null);
    if (tick === 0 && capsCache.has(key)) return;
    let live = true;
    get(`/api/tasks/${taskId}/capabilities?harness=${harness}`).then(
      (c) => { capsCache.set(key, c); if (live) setCaps(c); },
      () => live && !capsCache.has(key) && setCaps({ models: [], commands: [] }),
    );
    return () => { live = false; };
  }, [key, tick]);
  const refresh = useCallback(() => setTick((n) => n + 1), []);
  return [caps || { models: [], commands: [] }, refresh];
}

const kTokens = (n) => (n >= 1000 ? Math.round(n / 1000) + "k" : String(n));

function ContextMeter({ session }) {
  if (!session || !session.contextTokens) return null;
  const pct = session.contextWindow ? Math.min(100, Math.round((100 * session.contextTokens) / session.contextWindow)) : null;
  return html`<span class=${cls("ctx", pct >= 80 && "full")} title="Context window used by the last turn">
    ${pct !== null && html`<span class="gauge"><i style=${`width:${pct}%`}></i></span>`}
    ${kTokens(session.contextTokens)}${session.contextWindow ? " / " + kTokens(session.contextWindow) : " tokens"}
  </span>`;
}

export function Composer({ task, session, run, busy, busyText, placeholder, onSend, onStop }) {
  const [text, setText] = useState("");
  const [files, setFiles] = useState([]);
  const [uploading, setUploading] = useState(0);
  const [pick, setPick] = useState(0);
  const [prefs, setPrefs] = useState(sessionDefaults);
  const settings = session ? { harness: session.harness, model: session.model, effort: session.effort, permissionMode: session.permissionMode } : prefs;
  const [caps, refreshCaps] = useCapabilities(task.id, settings.harness);
  const ref = useRef(null);
  const fileInput = useRef(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [text]);

  const change = (k, v) => {
    const next = { ...settings, [k]: v };
    if (k === "harness") Object.assign(next, { model: "", effort: "" });
    if (session) {
      run(put(`/api/sessions/${session.id}/settings`, next));
    } else {
      setPrefs(next);
      try {
        localStorage.setItem(PREFS, JSON.stringify(next));
      } catch {
        /* private mode: not remembered */
      }
    }
  };

  const models = caps.models.filter((m) => m.value !== "default");
  const model = caps.models.find((m) => m.value === (settings.model || "default")) || caps.models[0];
  const efforts = (model && model.supportedEffortLevels) || [];

  const slash = /^[\/$](\S*)$/.exec(text);
  const matches = slash
    ? caps.commands.filter((c) => c.name.toLowerCase().includes(slash[1].toLowerCase())).slice(0, 8)
    : [];
  const choose = (c) => {
    setText((/^[\/$]/.test(c.name) ? c.name : "/" + c.name) + " ");
    setPick(0);
    ref.current && ref.current.focus();
  };

  const attach = async (list) => {
    for (const f of list) {
      setUploading((n) => n + 1);
      const r = await run(upload(`/api/tasks/${task.id}/attachments?name=${encodeURIComponent(f.name || "pasted.png")}`, f));
      setUploading((n) => n - 1);
      if (r) setFiles((prev) => [...prev, r.path]);
    }
  };

  const send = () => {
    if ((!text.trim() && !files.length) || busy || uploading) return;
    const body = text.trim() + (files.length ? `\n\nAttached files:\n${files.map((f) => "- " + f).join("\n")}` : "");
    onSend(body.trim(), settings);
    setText("");
    setFiles([]);
  };

  const select = (k, value, options, title) => html`<select class="pick" title=${title} value=${value} onChange=${(e) => change(k, e.target.value)}>
    ${options.map(([v, label]) => html`<option key=${v} value=${v}>${label}</option>`)}
  </select>`;

  return html`<div class="composer" onDragOver=${(e) => e.preventDefault()} onDrop=${(e) => { e.preventDefault(); attach([...e.dataTransfer.files]); }}>
    ${matches.length > 0 && html`<div class="slash">
      ${matches.map((c, i) => html`<button key=${c.name} aria-selected=${i === pick % matches.length} onMouseDown=${(e) => { e.preventDefault(); choose(c); }}>
        <span class="name">${/^[\/$]/.test(c.name) ? c.name : "/" + c.name}</span>${c.argumentHint && html`<span class="faint"> ${c.argumentHint}</span>`}
        <span class="desc">${c.description}</span>
      </button>`)}
    </div>`}
    ${(files.length > 0 || uploading > 0) && html`<div class="files">
      ${files.map((f) => html`<span key=${f} class="chip" title=${f}>${f.split("/").pop().replace(/^\d{6}\.\d{3}-/, "")}
        <button class="x" onClick=${() => setFiles(files.filter((x) => x !== f))}>✕</button></span>`)}
      ${uploading > 0 && html`<span class="faint">uploading…</span>`}
    </div>`}
    <textarea ref=${ref} rows="1" value=${text} placeholder=${busy ? busyText : placeholder + " · / for skills"}
      onInput=${(e) => {
        // Opening the menu reads the commands again, so newly added skills show up.
        if (/^[\/$]$/.test(e.target.value) && !/^[\/$]/.test(text)) refreshCaps();
        setText(e.target.value);
        setPick(0);
      }}
      onPaste=${(e) => { const fs = [...e.clipboardData.files]; if (fs.length) { e.preventDefault(); attach(fs); } }}
      onKeyDown=${(e) => {
        if (matches.length && (e.key === "ArrowDown" || e.key === "ArrowUp")) {
          e.preventDefault();
          setPick((p) => (p + (e.key === "ArrowDown" ? 1 : matches.length - 1)) % matches.length);
          return;
        }
        if (matches.length && (e.key === "Tab" || (e.key === "Enter" && !e.shiftKey))) {
          e.preventDefault();
          choose(matches[pick % matches.length]);
          return;
        }
        if (e.key === "Enter" && !e.shiftKey && !e.isComposing) {
          e.preventDefault();
          send();
        }
        if (e.key === "Escape") e.target.blur();
      }}></textarea>
    <div class="bar">
      <button class="key small ghost" title="Attach files (or drop or paste them)" onClick=${() => fileInput.current.click()}>＋ File</button>
      <input ref=${fileInput} type="file" multiple hidden onChange=${(e) => { attach([...e.target.files]); e.target.value = ""; }} />
      ${session
        ? html`<span class="pick fixed" title="A session keeps its harness">${HARNESSES[settings.harness] || settings.harness}</span>`
        : select("harness", settings.harness, Object.entries(HARNESSES), "Harness")}
      ${select("model", settings.model, [["", model && settings.model === "" ? model.displayName : "Default model"], ...models.map((m) => [m.value, m.displayName])], "Model")}
      ${efforts.length > 0 && select("effort", settings.effort, [["", "Default effort"], ...efforts.map((x) => [x, x])], "Effort")}
      ${select("permissionMode", settings.permissionMode, [...(settings.permissionMode === "" ? [["", "Harness settings"]] : []), ...Object.entries(MODES)], "When the agent wants to act")}
      <span class="grow"></span>
      <${ContextMeter} session=${session} />
      ${busy && onStop
        ? html`<button class="key small danger" onClick=${onStop}>■ Stop</button>`
        : html`<button class="key small primary" disabled=${(!text.trim() && !files.length) || uploading > 0} onClick=${send}>↵ Send</button>`}
    </div>
  </div>`;
}
