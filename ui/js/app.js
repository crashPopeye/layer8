import { html, render, useState, useEffect, useCallback } from "/vendor/preact-htm.js";
import { get, put } from "./api.js";
import { Sidebar, home } from "./side.js";
import { NewTask } from "./newtask.js";

const selectedFromHash = () => (location.hash.match(/^#\/task\/(.+)$/) || [])[1] || "";

/** useLive keeps projects and tasks fresh: it refetches on every SSE event and on reconnect. */
function useLive() {
  const [data, setData] = useState({ projects: [], tasks: [] });
  const [signedIn, setSignedIn] = useState(true);
  const [tick, setTick] = useState(0);
  const refresh = useCallback(async () => {
    try {
      const [projects, tasks] = await Promise.all([get("/api/projects"), get("/api/tasks")]);
      setData({ projects, tasks });
      setSignedIn(true);
    } catch (err) {
      if (err.status === 401) setSignedIn(false);
    }
  }, []);
  useEffect(() => {
    refresh();
    const events = new EventSource("/api/events");
    events.onopen = refresh;
    events.onmessage = () => {
      refresh();
      setTick((n) => n + 1);
    };
    return () => events.close();
  }, [refresh]);
  return { ...data, signedIn, tick };
}

function TaskView({ id, tick }) {
  const [task, setTask] = useState(null);
  const [error, setError] = useState("");
  useEffect(() => {
    get(`/api/tasks/${id}`).then(
      (t) => (setTask(t), setError("")),
      (err) => setError(err.message),
    );
  }, [id, tick]);
  if (error) return html`<div class="empty err">${error}</div>`;
  if (!task) return null;
  const setStatus = (status) => put(`/api/tasks/${task.id}`, { status }).catch((err) => setError(err.message));
  return html`<header>
      <span class="t">${task.title}</span>
      <span class="tag mono">${task.branch}</span>
      <span class="tag">${task.workflow}</span>
      ${task.status !== "active" && html`<span class="tag">${task.status}</span>`}
      <div class="actions">
        ${task.status === "active"
          ? html`<button onClick=${() => setStatus("done")}>Done</button><button onClick=${() => setStatus("archived")}>Archive</button>`
          : html`<button onClick=${() => setStatus("active")}>Reopen</button>`}
      </div>
    </header>
    <div class="body">
      <dl class="kv">
        <dt>worktree</dt><dd title=${task.worktree}>${home(task.worktree)}</dd>
        <dt>base</dt><dd>${task.baseRef}</dd>
        <dt>artifacts</dt><dd title=${task.artifactsDir}>${home(task.artifactsDir)}</dd>
        <dt>autonomous</dt><dd>${task.autonomous ? "yes" : "no"}</dd>
      </dl>
      <div class="lbl">task.md</div>
      <pre class="doc">${task.taskMd}</pre>
    </div>`;
}

function App() {
  const { projects, tasks, signedIn, tick } = useLive();
  const [selected, setSelected] = useState(selectedFromHash());
  const [newTaskIn, setNewTaskIn] = useState(null);
  useEffect(() => {
    const onHash = () => setSelected(selectedFromHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  const select = (id) => (location.hash = `#/task/${id}`);

  if (!signedIn) {
    return html`<div class="empty">
      <div class="lbl">Not signed in</div>
      <div>Open the UI with <code>make dev</code>, or visit <code>/auth?t=&lt;token&gt;</code> with the token from <code>~/.layer8/token</code>.</div>
    </div>`;
  }
  return html`<div class="app">
    <${Sidebar} projects=${projects} tasks=${tasks} selected=${selected} onSelect=${select} onNewTask=${setNewTaskIn} />
    <main>
      ${selected
        ? html`<${TaskView} id=${selected} tick=${tick} />`
        : html`<div class="empty"><div class="lbl">No task selected</div><div>Pick a task, or press ＋ next to a project to start one.</div></div>`}
    </main>
    ${newTaskIn &&
    html`<${NewTask}
      project=${newTaskIn}
      onClose=${() => setNewTaskIn(null)}
      onCreated=${(t) => (setNewTaskIn(null), select(t.id))}
    />`}
  </div>`;
}

render(html`<${App} />`, document.getElementById("root"));
