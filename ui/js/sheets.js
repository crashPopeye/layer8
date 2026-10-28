import { html, useState, useEffect, useMemo, useRef } from "/vendor/preact-htm.js";
import { get, post, put, del, withQuery, shell } from "./api.js";
import { home, useEscape, useFocus } from "./util.js";

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

export function NewTaskSheet({ state, projectId, onClose, onCreated, onSettings }) {
  const [folderId, setFolderId] = useState(() => {
    const candidates = state.folders.filter((f) => !projectId || f.projectId === projectId);
    const saved = store.get("layer8.lastFolder");
    return candidates.some((f) => f.id === saved) ? saved : candidates[0] && candidates[0].id;
  });
  const folder = state.folders.find((f) => f.id === folderId);
  const projects = state.projects.filter((p) => state.folders.some((f) => f.projectId === p.id));
  const siblings = folder ? state.folders.filter((f) => f.projectId === folder.projectId) : [];
  const [q, setQ] = useState("");
  const [issues, setIssues] = useState(null);
  const [linearError, setLinearError] = useState(null);
  const [cursor, setCursor] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState(null);
  const list = useRef(null);
  const search = useRef(null);
  useEscape(onClose);
  useFocus(search);

  useEffect(() => {
    if (!state.hasLinear) {
      setIssues([]);
      setLinearError("Linear is not connected");
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
    if (!folderId || busy) return;
    setBusy(true);
    setError(null);
    store.set("layer8.lastFolder", folderId);
    try {
      onCreated(await post("/api/tasks", { folderId, ...body }));
    } catch (e) {
      setError(e.message);
      setBusy(false);
    }
  };

  return html`
    <div class="scrim" onClick=${onClose}></div>
    <div class="sheet" onKeyDown=${(e) => {
      if (e.key === "ArrowDown") setCursor((c) => Math.min(c + 1, shown.length - 1));
      else if (e.key === "ArrowUp") setCursor((c) => Math.max(c - 1, 0));
      else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) q.trim() && create({ title: q.trim() });
      else if (e.key === "Enter" && shown[cursor]) create({ linearIssue: shown[cursor].identifier });
      else return;
      e.preventDefault();
    }}>
      <div class="sheet-head">
        <span class="display">New task</span><span style="flex:1"></span><span class="label">in</span>
        ${projects.length
          ? html`<div class="seg">${projects.map((p) => html`<button key=${p.id} aria-pressed=${!!folder && folder.projectId === p.id} onClick=${() => setFolderId(state.folders.find((f) => f.projectId === p.id).id)}>${p.name}</button>`)}</div>
            ${siblings.length > 1 && html`<div class="seg">${siblings.map((f) => html`<button key=${f.id} aria-pressed=${f.id === folderId} onClick=${() => setFolderId(f.id)}>${f.name}</button>`)}</div>`}`
          : html`<button class="key" onClick=${onSettings}>Add a project first</button>`}
      </div>
      <input ref=${search} class="field search" placeholder="Search Linear, or type ENG-123…  (⌘↵ for a plain task with this title)" value=${q} onInput=${(e) => setQ(e.target.value)} />
      <div class="sheet-body" ref=${list}>
        ${linearError && html`<div class="empty"><div class="signal">${linearError}</div>
          ${/not connected/.test(linearError) && html`<div class="btns"><button class="key" onClick=${onSettings}>Connect Linear</button></div>`}</div>`}
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

export function SettingsSheet({ state, run, upd, onCheckUpdate, onClose }) {
  const [cfg, setCfg] = useState(null);
  const [linearKey, setLinearKey] = useState("");
  const [ghToken, setGhToken] = useState("");
  const [projectName, setProjectName] = useState("");
  useEffect(() => {
    get("/api/config").then(setCfg);
  }, []);
  useEscape(onClose);
  if (!cfg) return null;
  const set = (k, v) => setCfg({ ...cfg, [k]: v });
  const persist = () => run(put("/api/config", { ...cfg, linearApiKey: linearKey.trim() || cfg.linearApiKey, githubToken: ghToken.trim() || cfg.githubToken }));
  const save = async () => {
    if ((await persist()) !== undefined) onClose();
  };
  const addFolder = async (projectId) => {
    const p = shell.available ? await shell.pickFolder() : prompt("Folder path");
    if (p) await run(post(`/api/projects/${projectId}/folders`, { path: p.trim() }));
  };
  const addProject = async () => {
    const p = await run(post("/api/projects", { name: projectName.trim() }));
    if (!p) return;
    setProjectName("");
    addFolder(p.id);
  };
  const toggle = (k) => html`<div class="seg"><button aria-pressed=${!!cfg[k]} onClick=${() => set(k, true)}>on</button><button aria-pressed=${!cfg[k]} onClick=${() => set(k, false)}>off</button></div>`;

  return html`
    <div class="scrim" onClick=${onClose}></div>
    <div class="sheet">
      <div class="sheet-head"><span class="display">Settings</span><span style="flex:1"></span>
        <button class="key" onClick=${onClose}>Esc</button><button class="key primary" onClick=${save}>Save</button></div>
      <div class="sheet-body"><div class="form">
        <span class="label">Projects</span>
        <div style="display:grid;grid-template-columns:minmax(0,1fr);gap:12px">
          ${state.projects.map((p) => html`<div key=${p.id} style="display:grid;grid-template-columns:minmax(0,1fr);gap:2px">
            <div class="list-row" style="cursor:default;padding-left:0">
              <span class="grow ellipsis">${p.name}</span>
              <button class="key small" onClick=${() => addFolder(p.id)}>Add folder…</button>
              <button class="key small danger" onClick=${() => confirm(`Remove the project ${p.name} and its tasks from layer8? Files and branches are not touched.`) && run(del(`/api/projects/${p.id}`))}>✕</button>
            </div>
            ${state.folders.filter((f) => f.projectId === p.id).map((f) => html`<div key=${f.id} class="list-row" style="cursor:default">
              <span style="width:120px" class="ellipsis">${f.name}</span><span class="grow faint ellipsis">${home(f.path)}</span>
              <span class="faint">${f.isGit ? "git · " + f.defaultBranch : "folder"}</span>
              <button class="key small danger" onClick=${() => confirm(`Remove ${f.name} and its tasks from ${p.name}? Files and branches are not touched.`) && run(del(`/api/folders/${f.id}`))}>✕</button>
            </div>`)}
          </div>`)}
          <div style="display:flex;gap:6px">
            <input class="field" placeholder="New project name" value=${projectName} onInput=${(e) => setProjectName(e.target.value)} onKeyDown=${(e) => e.key === "Enter" && projectName.trim() && addProject()} />
            <button class="key" disabled=${!projectName.trim()} onClick=${addProject}>Add project</button>
          </div>
        </div>

        <span class="label">Linear</span>
        ${state.linearOAuth
          ? html`<div style="display:flex;gap:8px;align-items:center"><span>Connected</span>
              <button class="key small danger" onClick=${() => run(del("/api/linear/connect"))}>Disconnect</button></div>`
          : html`<div><button class="key" onClick=${() => run(post("/api/linear/connect"))}>Connect Linear</button></div>`}
        ${!state.linearOAuth && html`<span class="hint">Opens Linear in your browser to approve access.</span>`}
        <span class="label">or an API key</span>
        <input class="field" type="password" placeholder=${cfg.linearApiKey === SET ? "•••••••• set — type to replace" : "lin_api_…"} value=${linearKey} onInput=${(e) => setLinearKey(e.target.value)} />
        <span class="hint">Linear → Settings → Security & access → Personal API keys. Used when Linear is not connected.</span>
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

        <span class="label">Permissions</span>
        <div class="seg">${[["acceptEdits", "accept edits"], ["default", "ask for everything"], ["settings", "my Claude settings"]].map(([v, l]) => html`<button key=${v} aria-pressed=${cfg.permissionMode === v} onClick=${() => set("permissionMode", v)}>${l}</button>`)}</div>
        <span class="hint">Accept edits: agents edit files in their worktree freely and ask before running commands not allowed by your Claude Code rules.</span>

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
          ${shell.available ? html`<button class="key" onClick=${onCheckUpdate}>Check now</button>` : ""}
          <span class="faint">service ${state.version}${upd && upd.current ? ` · app ${upd.current}` : ""}${upd && upd.available ? ` · ${upd.version} available` : upd && upd.checked ? " · up to date" : ""}${upd && upd.error ? " · " + upd.error : ""}</span>
        </div>
        <span class="label">Command line</span>
        <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
          ${shell.available && html`<button class="key" onClick=${async () => { const r = await run(shell.installCli()); if (r) alert(r); }}>Install the layer8 command</button>`}
          <span class="faint">Then run <code>layer8 help</code> in a terminal.</span>
        </div>
      </div></div>
    </div>`;
}
