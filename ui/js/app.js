import { html, render, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post, on, dispatchSessionEvents } from "./api.js";
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
    const offs = [
      on("layer8:state", refresh),
      on("layer8:events", dispatchSessionEvents),
    ];
    // Safety net: events emitted while the page was loading are not replayed.
    const t = setInterval(refresh, 15000);
    return () => {
      clearInterval(t);
      offs.forEach((p) => p.then((off) => off()));
    };
  }, [refresh]);
  return { state, up, refresh };
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
  const upd = state.update || {};

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
        ${upd.available &&
        html`<div class="banner">
          <span>layer8 <b>${upd.latest}</b> is available (you have ${state.version}).</span>
          ${upd.state === "downloading" || upd.state === "installing"
            ? html`<span class="dim">${upd.state}…</span>`
            : upd.canInstall
            ? html`<button class="key small primary" onClick=${() => run(post("/api/update/install"))}>Install and restart</button>`
            : html`<span class="faint">${upd.installHint}</span>`}
          ${upd.state === "error" && html`<span class="bad">${upd.error}</span>`}
          <span style="flex:1"></span>
          <a href=${upd.url}>release notes</a>
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
      ${sheet === "settings" && html`<${SettingsSheet} state=${state} run=${run} onClose=${() => setSheet(null)} />`}
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

render(html`<${App} />`, document.getElementById("root"));
