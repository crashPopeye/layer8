import { html, useState, useEffect, useMemo, useRef } from "/vendor/preact-htm.js";
import { get, post, put, del, withQuery } from "./api.js";
import { home } from "./util.js";

const store = {
  get(k) {
    try {
      return localStorage.getItem(k);
    } catch {
      return null;
    }
  },
  set(k, v) {
    try {
      localStorage.setItem(k, v);
    } catch {
      /* only the default selection is lost */
    }
  },
};

export function NewTaskSheet({ state, onClose, onCreated, onSettings }) {
  const workspaces = state.workspaces;
  const [wsId, setWsId] = useState(() => {
    const saved = store.get("layer8.lastWorkspace");
    return workspaces.some((w) => w.id === saved) ? saved : workspaces[0] && workspaces[0].id;
  });
  const [q, setQ] = useState("");
  const [issues, setIssues] = useState(null);
  const [linearError, setLinearError] = useState(null);
  const [cursor, setCursor] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const list = useRef(null);

  useEffect(() => {
    if (!state.hasLinear) {
      setIssues([]);
      setLinearError("Linear API key is not configured");
      return;
    }
    let live = true;
    const t = setTimeout(() => {
      get(withQuery("/api/linear/issues", { q: q.trim() })).then(
        (r) => {
          if (!live) return;
          setIssues(r || []);
          setLinearError(null);
          setCursor(0);
        },
        (e) => live && setLinearError(e.message),
      );
    }, q ? 220 : 0);
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [q]);

  // A typed identifier ("ENG-123") jumps to the top.
  const shown = useMemo(() => {
    const id = q.trim().toUpperCase();
    return [...(issues || [])].sort((a, b) => (b.identifier === id) - (a.identifier === id));
  }, [issues, q]);

  useEffect(() => {
    const el = list.current && list.current.querySelector('[aria-selected="true"]');
    if (el) el.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const create = async (body) => {
    if (!wsId || busy) return;
    setBusy(true);
    setError(null);
    store.set("layer8.lastWorkspace", wsId);
    try {
      onCreated(await post("/api/tasks", { workspaceId: wsId, ...body }));
    } catch (e) {
      setError(e.message);
      setBusy(false);
    }
  };

  return html`
    <div class="scrim" onClick=${onClose}></div>
    <div class="sheet" onKeyDown=${(e) => {
      if (e.key === "Escape") onClose();
      else if (e.key === "ArrowDown") setCursor((c) => Math.min(c + 1, shown.length - 1));
      else if (e.key === "ArrowUp") setCursor((c) => Math.max(c - 1, 0));
      else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) q.trim() && create({ title: q.trim() });
      else if (e.key === "Enter" && shown[cursor]) create({ linearIssue: shown[cursor].identifier });
      else return;
      e.preventDefault();
    }}>
      <div class="sheet-head">
        <span class="display">New task</span><span style="flex:1"></span><span class="label">in</span>
        ${workspaces.length
          ? html`<div class="seg">${workspaces.map((w) => html`<button key=${w.id} aria-pressed=${w.id === wsId} onClick=${() => setWsId(w.id)}>${w.name}</button>`)}</div>`
          : html`<button class="key" onClick=${onSettings}>Add a workspace first</button>`}
      </div>
      <input class="field search" autofocus placeholder="Search Linear, or type ENG-123…  (⌘↵ for a plain task with this title)" value=${q} onInput=${(e) => setQ(e.target.value)} />
      <div class="sheet-body" ref=${list}>
        ${linearError && html`<div class="empty"><div class="signal">${linearError}</div>
          ${/not configured/.test(linearError) && html`<div class="btns"><button class="key" onClick=${onSettings}>Set Linear API key</button></div>`}</div>`}
        ${!linearError && issues === null && html`<div class="empty">Loading your tickets…</div>`}
        ${!linearError && issues && !issues.length && html`<div class="empty">No matching tickets.</div>`}
        ${shown.map((i, n) => html`<div key=${i.id} class="issue" aria-selected=${n === cursor} onMouseMove=${() => setCursor(n)} onClick=${() => create({ linearIssue: i.identifier })}>
          <span class="sq" style=${{ background: i.state.color }}></span>
          <span class="id">${i.identifier}</span>
          <span class="ellipsis">${i.title}</span>
          <span class="state">${i.state.name}</span>
        </div>`)}
      </div>
      <div class="sheet-head" style="border-top:1px solid var(--rule);border-bottom:none">
        ${error ? html`<span class="bad selectable">${error}</span>`
          : busy ? html`<span class="dim">Creating worktree…</span>`
          : html`<span class="faint"><kbd>↵</kbd> start from ticket · <kbd>⌘↵</kbd> plain task titled “${q || "…"}” · <kbd>esc</kbd> close</span>`}
      </div>
    </div>`;
}

const TERMINALS = ["terminal", "iterm", "ghostty", "wezterm", "kitty", "custom"];
const EDITORS = ["code", "cursor", "zed", "idea", "custom"];
const SET = "__set__";

export function SettingsSheet({ state, run, onClose }) {
  const [cfg, setCfg] = useState(null);
  const [linearKey, setLinearKey] = useState("");
  const [ghToken, setGhToken] = useState("");
  const [path, setPath] = useState("");
  useEffect(() => {
    get("/api/config").then(setCfg);
  }, []);
  if (!cfg) return null;
  const set = (k, v) => setCfg({ ...cfg, [k]: v });
  const save = async () => {
    const ok = await run(put("/api/config", { ...cfg, linearApiKey: linearKey.trim() || cfg.linearApiKey, githubToken: ghToken.trim() || cfg.githubToken }));
    if (ok !== undefined) onClose();
  };
  const addWorkspace = async (p) => {
    if (!p) return;
    if (await run(post("/api/workspaces", { path: p }))) setPath("");
  };
  const pick = async () => {
    const r = await run(post("/api/pick-folder"));
    if (r && r.path) addWorkspace(r.path);
  };
  const toggle = (k) => html`<div class="seg"><button aria-pressed=${!!cfg[k]} onClick=${() => set(k, true)}>on</button><button aria-pressed=${!cfg[k]} onClick=${() => set(k, false)}>off</button></div>`;
  const upd = state.update || {};

  return html`
    <div class="scrim" onClick=${onClose}></div>
    <div class="sheet" onKeyDown=${(e) => e.key === "Escape" && onClose()}>
      <div class="sheet-head"><span class="display">Settings</span><span style="flex:1"></span>
        <button class="key" onClick=${onClose}>Esc</button><button class="key primary" onClick=${save}>Save</button></div>
      <div class="sheet-body"><div class="form">
        <span class="label">Workspaces</span>
        <div style="display:grid;gap:4px">
          ${state.workspaces.map((w) => html`<div key=${w.id} class="list-row" style="cursor:default;padding-left:0">
            <span style="width:120px" class="ellipsis">${w.name}</span><span class="grow faint">${home(w.path)}</span>
            <span class="faint">${w.isGit ? "git · " + w.defaultBranch : "folder"}</span>
            <button class="key small danger" onClick=${() => confirm(`Remove ${w.name} and its tasks from layer8? Files and branches are not touched.`) && run(del(`/api/workspaces/${w.id}`))}>✕</button>
          </div>`)}
          <div style="display:flex;gap:6px">
            <button class="key" onClick=${pick}>Choose folder…</button>
            <input class="field" placeholder="or paste /path/to/folder" value=${path} onInput=${(e) => setPath(e.target.value)} onKeyDown=${(e) => e.key === "Enter" && addWorkspace(path.trim())} />
          </div>
        </div>

        <span class="label">Linear API key</span>
        <input class="field" type="password" placeholder=${cfg.linearApiKey === SET ? "•••••••• set — type to replace" : "lin_api_…"} value=${linearKey} onInput=${(e) => setLinearKey(e.target.value)} />
        <span class="hint">Linear → Settings → Security & access → Personal API keys.</span>
        <span class="label">Linear sub-issues</span>${toggle("createLinearSubIssues")}
        <span class="hint">Split into PRs creates one sub-issue per PR, so each PR links to its own issue.</span>

        <span class="label">GitHub token</span>
        <input class="field" type="password" placeholder=${cfg.githubToken === SET ? "•••••••• set — type to replace" : "empty = use `gh auth token`"} value=${ghToken} onInput=${(e) => setGhToken(e.target.value)} />
        <span class="hint">Used for PRs, CI, reviews and updates. Leave empty if you use the gh CLI.</span>
        <span class="label">Draft PRs</span>${toggle("draftPRs")}

        <span class="label">Terminal</span>
        <div class="seg">${TERMINALS.map((t) => html`<button key=${t} aria-pressed=${cfg.terminal === t} onClick=${() => set("terminal", t)}>${t}</button>`)}</div>
        ${cfg.terminal === "custom" && html`<span></span><input class="field" placeholder="alacritty --working-directory {cwd} -e sh -c {cmd}" value=${cfg.terminalCommand} onInput=${(e) => set("terminalCommand", e.target.value)} />`}
        <span class="label">tmux per worktree</span>${toggle("useTmux")}
        <span class="hint">Shells and dev servers survive closing the terminal window.</span>
        <span class="label">Editor</span>
        <div class="seg">${EDITORS.map((t) => html`<button key=${t} aria-pressed=${cfg.editor === t} onClick=${() => set("editor", t)}>${t}</button>`)}</div>
        ${cfg.editor === "custom" && html`<span></span><input class="field" placeholder="open -a MacVim {path}" value=${cfg.editorCommand} onInput=${(e) => set("editorCommand", e.target.value)} />`}

        <span class="label">Parallel agents</span>
        <input class="field" type="number" min="1" max="32" style="width:90px" value=${cfg.maxConcurrentSessions} onInput=${(e) => set("maxConcurrentSessions", Math.max(1, Number(e.target.value) || 1))} />
        <span class="label">Model</span>
        <input class="field" placeholder="empty = Claude Code's default" value=${cfg.model} onInput=${(e) => set("model", e.target.value)} />
        <span class="label">Auto-start next PR</span>${toggle("autoStartSlices")}
        <span class="hint">When a PR's session finishes with commits, start the PRs stacked on it.</span>

        <span class="label">Sync with main every</span>
        <div style="display:flex;gap:8px;align-items:center"><input class="field" type="number" min="0" style="width:90px" value=${cfg.syncIntervalMinutes} onInput=${(e) => set("syncIntervalMinutes", Math.max(0, Number(e.target.value) || 0))} /><span class="faint">minutes (0 = off)</span></div>
        <span class="label">Push after sync</span>${toggle("pushAfterSync")}
        <span class="hint">Force-push (with lease) rebased branches that are already on GitHub, so open PRs stay current.</span>
        <span class="label">Worktrees in</span>
        <input class="field" value=${cfg.worktreeRoot} onInput=${(e) => set("worktreeRoot", e.target.value)} />

        <span class="label">Updates</span>
        <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
          ${toggle("autoUpdate")}
          <button class="key" onClick=${() => run(post("/api/update/check"))}>Check now</button>
          <span class="faint">v${state.version}${upd.latest ? ` · latest ${upd.latest}` : ""}${upd.state === "error" ? " · " + upd.error : ""}</span>
        </div>
      </div></div>
    </div>`;
}
