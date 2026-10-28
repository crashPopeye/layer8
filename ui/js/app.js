import { html, render, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post, on, onOpen, startEvents, shell, connect, rememberToken } from "./api.js";
import { useKeys, home, cls } from "./util.js";
import { TaskView } from "./task.js";
import { NewTaskSheet, SettingsSheet } from "./sheets.js";

const empty = { workspaces: [], tasks: [], slices: [], sessions: [], approvals: [], running: 0, queued: 0, maxRunning: 6, update: {} };

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

  useKeys({ n: () => setSheet("new"), ",": () => setSheet("settings") }, sheet === null);

  const task = state.tasks.find((t) => t.id === taskId) || null;
  const waiting = state.approvals.length;

  return html`
    <div class="frame">
      <header class="top">
        <div class="wordmark">layer<b>8</b></div>
        <div class="grow"></div>
        <div title="Agents working / limit">
          <span class="label">run</span>
          <span class="meter">${Array.from({ length: state.maxRunning }, (_, i) => html`<i class=${i < state.running ? "on" : ""} />`)}</span>
          <span class="dim">${state.running}</span>
          ${state.queued > 0 && html`<span class="faint">+${state.queued} queued</span>`}
        </div>
        <div><span class="label">approve</span><span class=${waiting ? "signal" : "faint"}>${waiting}</span></div>
        <div title=${up ? "Connected" : "Backend not responding"}>
          <span class=${cls("sq", up ? "idle" : "failed")}></span><span class="label">${/^\d/.test(state.version || "") ? "v" + state.version : state.version || "?"}</span>
        </div>
        <div><button class="key primary" onClick=${() => setSheet("new")}><kbd>N</kbd> New task</button></div>
      </header>

      <nav class="rail">
        <${Rail} state=${state} selected=${taskId} onSelect=${setTaskId} onNew=${() => setSheet("new")} onSettings=${() => setSheet("settings")} />
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
          ? html`<${TaskView} key=${task.id} state=${state} task=${task} run=${run} keysEnabled=${sheet === null} />`
          : html`<div class="empty">
              <div class="display">No task open</div>
              Press <kbd>N</kbd> to start one from a Linear ticket, or pick one on the left.
            </div>`}
      </main>

      <footer class="foot">
        <span><kbd>N</kbd> new task</span>
        <span><kbd>R</kbd>/<kbd>P</kbd>/<kbd>I</kbd> research · plan · implement</span>
        <span><kbd>T</kbd> terminal</span>
        <span><kbd>E</kbd> editor</span>
        <span><kbd>A</kbd>/<kbd>D</kbd> allow · deny</span>
        <span><kbd>1</kbd>–<kbd>4</kbd> tabs</span>
        <span style="flex:1"></span>
        <button class="key small ghost" onClick=${() => setSheet("settings")}><kbd>,</kbd> settings</button>
      </footer>

      ${sheet === "new" &&
      html`<${NewTaskSheet}
        state=${state}
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

function Rail({ state, selected, onSelect, onNew, onSettings }) {
  if (!state.workspaces.length) {
    return html`<div class="empty">
      <div class="display">Empty</div>
      Add a folder as a workspace. Any folder works; git repos also get a worktree per task and per PR.
      <div class="btns"><button class="key" onClick=${onSettings}>Add a workspace</button></div>
    </div>`;
  }
  const active = state.tasks.filter((t) => t.status === "active");
  return state.workspaces.map((ws) => {
    const tasks = active.filter((t) => t.workspaceId === ws.id);
    return html`<section key=${ws.id}>
      <div class="ws-head"><span class="label">${ws.name}</span><span class="faint ellipsis" style="font-size:10.5px">${home(ws.path)}</span></div>
      ${tasks.map((t) => {
        const sessions = state.sessions.filter((s) => s.taskId === t.id);
        const slices = state.slices.filter((x) => x.taskId === t.id);
        const needs = state.approvals.some((a) => sessions.some((s) => s.id === a.sessionId));
        return html`<div key=${t.id} class="task-row" aria-selected=${t.id === selected} onClick=${() => onSelect(t.id)}>
          <span class="id">${t.linear ? t.linear.identifier : "—"}</span>
          <span class="ellipsis">${t.title}</span>
          <span class="lights">${sessions.slice(-6).map((s) => html`<span key=${s.id} class=${cls("sq", s.status)} title=${`${s.title}: ${s.status}`}></span>`)}</span>
          <span class="sub ellipsis">
            ${needs ? html`<span class="waitc">needs approval · </span>` : ""}
            ${slices.length ? `${slices.filter((x) => x.prState === "MERGED").length}/${slices.length} PRs merged` : t.branch || "no git"}
          </span>
        </div>`;
      })}
      ${!tasks.length && html`<div class="task-row" onClick=${onNew}><span class="faint">+</span><span class="faint">new task</span></div>`}
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
