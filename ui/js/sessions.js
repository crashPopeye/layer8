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
  const decide = (allow, always = false) => run(post(`/api/approvals/${approval.id}`, { allow, always, message: allow ? "" : reason }));
  const always = approval.always || [];
  return html`<div class="approval"><div>
    <div class="what">Approve ${approval.toolName}?</div>
    <pre class="selectable">${describe(approval)}</pre>
    <div class="row">
      <button class="key primary" onClick=${() => decide(true)}>Allow</button>
      ${always.length > 0 && html`<button class="key" title=${`Adds ${always.join(", ")} to your Claude Code settings (~/.claude/settings.json), so it isn't asked again in any task`}
        onClick=${() => decide(true, true)}>Always allow ${always.join(", ")}</button>`}
      <input class="field" placeholder="Or tell the agent what to do instead…" value=${reason}
        onInput=${(e) => setReason(e.target.value)} onKeyDown=${(e) => e.key === "Enter" && decide(false)} />
      <button class="key danger" onClick=${() => decide(false)}>Deny</button>
    </div>
  </div></div>`;
}

export const HARNESSES = { claude: "Claude Code", codex: "Codex" };
const MODES = { default: "Ask", acceptEdits: "Auto-accept edits", auto: "Auto-accept", bypassPermissions: "Bypass permissions" };
const MODE_HINTS = {
  default: "Asks before edits and commands",
  acceptEdits: "File edits go through, commands ask",
  auto: "Acts on its own, asks only for risky actions",
  bypassPermissions: "Never asks",
  "": "Follows your Claude Code settings",
};
const EFFORTS = { low: "Low", medium: "Medium", high: "High", xhigh: "Extra high", max: "Max" };
const MODE_SHORT = { default: "Ask", acceptEdits: "Edits", auto: "Auto", bypassPermissions: "Bypass", "": "Settings" };

const ICONS = {
  plus: "M8 3v10M3 8h10",
  shield: "M8 1.8 3 3.6v4.1c0 3.1 2.1 5.3 5 6.5 2.9-1.2 5-3.4 5-6.5V3.6L8 1.8Z",
  chevron: "m4.5 6.5 3.5 3.5 3.5-3.5",
  up: "M8 13V3.5M4 7.5l4-4 4 4",
  check: "m3.5 8.5 3 3 6-7",
};
const Icon = ({ name, size = 14 }) => html`<svg class="icon" width=${size} height=${size} viewBox="0 0 16 16" fill="none" stroke="currentColor"
  stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d=${ICONS[name]} /></svg>`;

/** A popover above its button; closes on an outside click or Escape. */
function Menu({ label, title, cls: c, align = "left", render }) {
  const [open, setOpen] = useState(false);
  const box = useRef(null);
  useEffect(() => {
    if (!open) return;
    const away = (e) => box.current && !box.current.contains(e.target) && setOpen(false);
    const esc = (e) => e.key === "Escape" && (e.stopPropagation(), setOpen(false));
    document.addEventListener("mousedown", away);
    document.addEventListener("keydown", esc, true);
    return () => { document.removeEventListener("mousedown", away); document.removeEventListener("keydown", esc, true); };
  }, [open]);
  return html`<div class="menu-anchor" ref=${box}>
    <button class=${cls("chipbtn", c, open && "on")} title=${title} aria-expanded=${open} onClick=${() => setOpen(!open)}>${label}</button>
    ${open && html`<div class=${cls("menu", align)} role="menu">${render(() => setOpen(false))}</div>`}
  </div>`;
}

const shortModel = (m) => (m ? m.displayName.replace(/\s*\(.*\)\s*$/, "") : "Default model");
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
    if (!harness || (tick === 0 && capsCache.has(key))) return;
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
  const tokens = kTokens(session.contextTokens) + (session.contextWindow ? " / " + kTokens(session.contextWindow) : "") + " tokens";
  return html`<span class=${cls("ctx", pct >= 80 && "full")} title=${`Context window used by the last turn: ${tokens}`}>
    ${pct !== null
      ? html`<svg width="14" height="14" viewBox="0 0 16 16" aria-hidden="true"><circle cx="8" cy="8" r="6" fill="none" stroke="var(--raise-2)" stroke-width="2" />
          <circle cx="8" cy="8" r="6" fill="none" stroke="currentColor" stroke-width="2" pathLength="100" stroke-dasharray=${`${pct} 100`} transform="rotate(-90 8 8)" /></svg>${pct}%`
      : tokens}
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
  // The other harness's models, listed in the model menu of a new session.
  const other = session ? null : Object.keys(HARNESSES).find((h) => h !== settings.harness);
  const [otherCaps] = useCapabilities(task.id, other);
  const ref = useRef(null);
  const fileInput = useRef(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [text]);

  const change = (k, v, extra) => {
    const next = { ...settings, [k]: v };
    if (k === "harness") Object.assign(next, { model: "", effort: "" }, extra);
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

  const [more, setMore] = useState({});
  const modelRows = (harness, list, close) => {
    const current = harness === settings.harness;
    const all = list.models.length ? list.models : [{ value: "default", displayName: "Default model" }];
    // The first few (default and the newest) up front, the rest behind "More models".
    const picked = all.findIndex((m) => current && (m.value === "default" ? "" : m.value) === settings.model);
    const cut = more[harness] || all.length <= 6 ? all.length : Math.max(5, picked + 1);
    const rows = all.slice(0, cut).map((m) => {
      const value = m.value === "default" ? "" : m.value;
      const on = current && settings.model === value;
      return html`<button key=${harness + m.value} class="item" role="menuitemradio" aria-checked=${on} title=${m.description || ""}
        onClick=${() => { if (current) change("model", value); else change("harness", harness, { model: value }); close(); }}>
        <span>${m.displayName}</span>${on && html`<${Icon} name="check" />`}
      </button>`;
    });
    if (cut < all.length) {
      rows.push(html`<button key=${harness + "-more"} class="item more" onClick=${() => setMore({ ...more, [harness]: true })}>
        <span>More ${HARNESSES[harness]} models</span><span class="faint">${all.length - cut}</span>
      </button>`);
    }
    return rows;
  };

  const modelMenu = html`<${Menu} align="right" title="Model and effort" label=${html`${shortModel(model)}${session ? "" : html` <span class="faint">· ${HARNESSES[settings.harness]}</span>`} <${Icon} name="chevron" size=${12} />`}
    render=${(close) => html`
      <div class="head">${HARNESSES[settings.harness] || settings.harness}</div>
      ${modelRows(settings.harness, caps, close)}
      ${other && otherCaps.models.length > 0 && html`<div class="sep"></div><div class="head">${HARNESSES[other]}</div>${modelRows(other, otherCaps, close)}`}
      ${efforts.length > 0 && html`<div class="sep"></div><div class="head">Effort</div>
        <div class="seg">${[["", "Default"], ...efforts.map((x) => [x, EFFORTS[x] || x])].map(([v, l]) => html`<button key=${v}
          aria-pressed=${settings.effort === v} onClick=${() => change("effort", v)}>${l}</button>`)}</div>`}`} />`;

  const modes = [...(settings.permissionMode === "" ? [""] : []), ...Object.keys(MODES)];
  const modeMenu = html`<${Menu} title="When the agent wants to act" label=${html`<${Icon} name="shield" /> ${MODE_SHORT[settings.permissionMode] ?? settings.permissionMode}`}
    render=${(close) => html`<div class="head">When the agent wants to act</div>
      ${modes.map((v) => html`<button key=${v} class="item" role="menuitemradio" aria-checked=${settings.permissionMode === v}
        onClick=${() => { change("permissionMode", v); close(); }}>
        <span><span>${MODES[v] || "Harness settings"}</span><span class="hint">${MODE_HINTS[v]}</span></span>
        ${settings.permissionMode === v && html`<${Icon} name="check" />`}
      </button>`)}`} />`;

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
      <button class="chipbtn round" title="Attach files (or drop or paste them)" onClick=${() => fileInput.current.click()}><${Icon} name="plus" /></button>
      <input ref=${fileInput} type="file" multiple hidden onChange=${(e) => { attach([...e.target.files]); e.target.value = ""; }} />
      ${modeMenu}
      <${ContextMeter} session=${session} />
      <span class="grow"></span>
      ${modelMenu}
      ${busy && onStop
        ? html`<button class="send stop" title="Stop" onClick=${onStop}><span class="sq"></span></button>`
        : html`<button class="send" title="Send (↵)" disabled=${(!text.trim() && !files.length) || uploading > 0} onClick=${send}><${Icon} name="up" size=${15} /></button>`}
    </div>
  </div>`;
}
