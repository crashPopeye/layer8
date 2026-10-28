import { html, render, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post, on, onOpen, startEvents, shell, connect, rememberToken } from "./api.js";
import { home, cls } from "./util.js";
import { TaskView } from "./task.js";
import { NewTaskSheet, SettingsSheet } from "./sheets.js";

const empty = { projects: [], folders: [], tasks: [], slices: [], sessions: [], approvals: [], running: 0, queued: 0, maxRunning: 6, update: {} };

function useAppState() {
  const [state, setState] = useState(empty);
  const [up, setUp] = useState(true);
  const inFlight = useRef(false);
  const again = useRef(false);
  const refresh = useCallback(async () => {
    if (inFlight.current) {
      again.current = true;
      return;
    }
    inFlight.current = true;
    try {
      do {
        again.current = false;
        setState(await get("/api/state"));
        setUp(true);
      } while (again.current);
    } catch {
      setUp(false);
    } finally {
      inFlight.current = false;
    }
  }, []);
  useEffect(() => {
    refresh();
    const offs = [on("layer8:state", refresh), onOpen(refresh)];
    startEvents(setUp);
    // Safety net for anything missed while the stream was down.
    const t = setInterval(refresh, 15000);
    return () => {
      clearInterval(t);
      offs.forEach((off) => off());
    };
  }, [refresh]);
  return { state, up, refresh };
}

/** Updates come from the Tauri shell (signed, verified by Tauri's updater). */
function useUpdate() {
  const [upd, setUpd] = useState(null);
  const check = useCallback(async () => {
    try {
      setUpd(await shell.checkUpdate());
    } catch (e) {
      setUpd({ error: String(e) });
    }
  }, []);
  useEffect(() => {
    if (!shell.available) return;
    check();
    const t = setInterval(check, 6 * 3600 * 1000);
    return () => clearInterval(t);
  }, [check]);
  return [upd, check, setUpd];
}

// External links open in the browser through Go (http/https only).
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[href]");
  if (!a) return;
  const href = a.getAttribute("href");
  if (/^https?:/i.test(href)) {
    e.preventDefault();
    post("/api/open-url", { url: href }).catch(() => {});
  } else if (!href.startsWith("#")) {
    e.preventDefault();
  }
});

function App() {
  const { state, up, refresh } = useAppState();
  const [upd, checkUpdate, setUpd] = useUpdate();
  const [installing, setInstalling] = useState(false);
  const [taskId, setTaskId] = useState(null);
  const [sheet, setSheet] = useState(null);
  const [newIn, setNewIn] = useState(null);
  const openNew = (projectId = null) => {
    setNewIn(projectId);
    setSheet("new");
  };
  const [error, setError] = useState(null);

  const run = useCallback(async (p) => {
    try {
      setError(null);
      const r = await p;
      refresh();
      return r;
    } catch (e) {
      setError(e.message);
      return undefined;
    }
  }, [refresh]);

  useEffect(() => {
    if (taskId && !state.tasks.some((t) => t.id === taskId && t.status === "active")) setTaskId(null);
  }, [state.tasks, taskId]);

  const task = state.tasks.find((t) => t.id === taskId) || null;
  const waiting = state.approvals.length;

  return html`
    <div class="frame">
      <header class="top" data-tauri-drag-region>
        <div class="wordmark" data-tauri-drag-region><img src="/logo.png" alt="" class="logo" data-tauri-drag-region /><span data-tauri-drag-region>layer<b>8</b></span></div>
        <div class="grow" data-tauri-drag-region></div>
        <div title="Agents working / limit">
          <span class="label">Agents</span>
          <span class="meter">${Array.from({ length: state.maxRunning }, (_, i) => html`<i class=${i < state.running ? "on" : ""} />`)}</span>
          <span class="dim">${state.running}</span>
          ${state.queued > 0 && html`<span class="faint">+${state.queued} queued</span>`}
        </div>
        <div><span class="label">Approvals</span><span class=${waiting ? "chip signal" : "faint"}>${waiting}</span></div>
        <div title=${up ? "Connected" : "Backend not responding"}>
          <span class=${cls("sq", up ? "idle" : "failed")}></span><span class="label">${/^\d/.test(state.version || "") ? "v" + state.version : state.version || "?"}</span>
        </div>
        <div><button class="key primary" onClick=${() => setSheet("new")}>New task</button></div>
      </header>

      <nav class="rail">
        <${Rail} state=${state} selected=${taskId} onSelect=${setTaskId} onNew=${openNew} onSettings=${() => setSheet("settings")} run=${run} />
      </nav>

      <main class="main">
        ${upd && upd.available &&
        html`<div class="banner">
          <span>layer8 <b>${upd.version}</b> is available.</span>
          ${installing
            ? html`<span class="dim">Downloading and installing… layer8 restarts by itself.</span>`
            : html`<button class="key small primary" onClick=${async () => {
                if (state.running + state.queued > 0 && !confirm("Agents are working. The window restarts, but agents keep running in the background service, which switches to the new version once they finish. Install now?")) return;
                setInstalling(true);
                try {
                  await shell.installUpdate();
                } catch (e) {
                  setInstalling(false);
                  setUpd({ ...upd, error: String(e) });
                }
              }}>Install and restart</button>`}
          ${upd.error && html`<span class="bad">${upd.error}</span>`}
        </div>`}
        ${task
          ? html`<${TaskView} key=${task.id} state=${state} task=${task} run=${run} />`
          : html`<div class="empty">
              <div class="display">No task open</div>
              Start one from a Linear ticket with <b>New task</b>, or pick one on the left.
            </div>`}
      </main>

      <footer class="foot">
        <button class="key small ghost" onClick=${() => setSheet("settings")}>Settings</button>
      </footer>

      ${sheet === "new" &&
      html`<${NewTaskSheet}
        state=${state}
        projectId=${newIn}
        onClose=${() => setSheet(null)}
        onSettings=${() => setSheet("settings")}
        onCreated=${(t) => {
          setSheet(null);
          refresh();
          setTaskId(t.id);
        }}
      />`}
      ${sheet === "settings" && html`<${SettingsSheet} state=${state} run=${run} upd=${upd} onCheckUpdate=${checkUpdate} onClose=${() => setSheet(null)} />`}
      ${error && html`<div class="toast" onClick=${() => setError(null)}><div class="label bad">error</div>${error}</div>`}
    </div>
  `;
}

function Rail({ state, selected, onSelect, onNew, onSettings, run }) {
  if (!state.folders.length) {
    return html`<div class="empty">
      <div class="display">Empty</div>
      Create a project and add its folders. Any folder works; git repos also get a worktree per task and per PR.
      <div class="btns"><button class="key" onClick=${onSettings}>Add a project</button></div>
    </div>`;
  }
  const active = state.tasks.filter((t) => t.status === "active");
  return state.projects.map((p) => {
    const folders = state.folders.filter((f) => f.projectId === p.id);
    const folderName = Object.fromEntries(folders.map((f) => [f.id, f.name]));
    const tasks = active.filter((t) => t.folderId in folderName);
    return html`<section key=${p.id}>
      <div class="project-head">
        <span class="label">${p.name}</span>
        <span class="faint ellipsis">${folders.map((f) => f.name).join(" · ") || "no folders"}</span>
        <button class="key small ghost" title=${`New task in ${p.name}`} disabled=${!folders.length} onClick=${() => onNew(p.id)}>+</button>
      </div>
      ${tasks.map((t) => {
        const sessions = state.sessions.filter((s) => s.taskId === t.id);
        const slices = state.slices.filter((x) => x.taskId === t.id);
        const needs = state.approvals.some((a) => sessions.some((s) => s.id === a.sessionId));
        return html`<div key=${t.id} class="task-row" aria-selected=${t.id === selected} onClick=${() => onSelect(t.id)}>
          <span class="id">${t.linear ? t.linear.identifier : "—"}</span>
          <span class="ellipsis">${t.title}</span>
          <span class="lights">${sessions.slice(-6).map((s) => html`<span key=${s.id} class=${cls("sq", s.status)} title=${`${s.title}: ${s.status}`}></span>`)}</span>
          <button class="key small ghost archive" title="Archive this task. Branches and worktrees are kept." onClick=${(e) => {
            e.stopPropagation();
            if (confirm(`Archive "${t.title}"? Branches and worktrees are kept.`)) run(post(`/api/tasks/${t.id}/archive`, { removeWorktrees: false }));
          }}>Archive</button>
          <span class="sub ellipsis">
            ${needs ? html`<span class="waitc">needs approval · </span>` : ""}
            ${folders.length > 1 ? folderName[t.folderId] + " · " : ""}
            ${slices.length ? `${slices.filter((x) => x.prState === "MERGED").length}/${slices.length} PRs merged` : t.branch || "no git"}
          </span>
        </div>`;
      })}
      ${!tasks.length && folders.length > 0 && html`<div class="task-row" onClick=${() => onNew(p.id)}><span class="faint">+</span><span class="faint">new task</span></div>`}
    </section>`;
  });
}

function Connect({ onDone }) {
  const [token, setToken] = useState("");
  return html`<div class="empty" style="max-width:520px;margin:80px auto">
    <div class="display" style="font-size:28px">layer<span class="signal">8</span></div>
    <p>Paste the daemon token from <code>~/.layer8/token</code> (browser development mode).</p>
    <input class="field" autofocus value=${token} onInput=${(e) => setToken(e.target.value.trim())} />
    <div class="btns"><button class="key primary" disabled=${!token} onClick=${() => { rememberToken(token); onDone(); }}>Connect</button></div>
  </div>`;
}

function Root() {
  const [ready, setReady] = useState(null); // null = connecting, false = needs token, true = ok
  const [error, setError] = useState(null);
  useEffect(() => {
    connect().then((d) => setReady(!!d), (e) => setError(String(e)));
  }, []);
  if (error) return html`<div class="empty"><div class="display">layer8 can't start its service</div><pre class="selectable bad">${error}</pre>
    <p>Details are in <code>~/.layer8/logs/layer8d.log</code>.</p></div>`;
  if (ready === null) return html`<div class="empty">Starting…</div>`;
  if (!ready) return html`<${Connect} onDone=${() => setReady(true)} />`;
  return html`<${App} />`;
}

render(html`<${Root} />`, document.getElementById("root"));
