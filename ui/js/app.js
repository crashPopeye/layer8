import { html, render, useState, useEffect, useCallback } from "/vendor/preact-htm.js";
import { get, put } from "./api.js";
import { useEvents, useSessions, useActiveSessions } from "./events.js";
import { Sidebar, home } from "./side.js";
import { NewTask } from "./newtask.js";
import { SessionView, NewSession, StatusDot } from "./conversation.js";
import { RightPanel } from "./panel.js";

/** route reads #/task/<id>[/s/<sessionId> | /new] from the URL hash. */
function route() {
  const [, task = "", rest = ""] = location.hash.match(/^#\/task\/([^/]+)(?:\/(.*))?$/) || [];
  return { task, session: rest.startsWith("s/") ? rest.slice(2) : "", newSession: rest === "new" };
}

const go = (task, rest = "") => (location.hash = `#/task/${task}${rest ? "/" + rest : ""}`);

/** useLive keeps projects and tasks fresh: it refetches when one changes and on reconnect. */
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
  useEffect(() => void refresh(), [refresh]);
  useEvents((e) => {
    if (["open", "project", "task"].includes(e.kind)) {
      refresh();
      setTick((n) => n + 1);
    }
  });
  return { ...data, signedIn, tick };
}

function TaskDetails({ task, parent }) {
  return html`<div class="body">
    <dl class="kv">
      ${parent && html`<dt>parent</dt><dd><a href=${`#/task/${parent.id}`}>${parent.title}</a> · .layer8/task/parent/</dd>`}
      <dt>worktree</dt><dd title=${task.worktree}>${home(task.worktree)}</dd>
      <dt>base</dt><dd>${task.baseRef}</dd>
      <dt>artifacts</dt><dd title=${task.artifactsDir}>${home(task.artifactsDir)}</dd>
      <dt>autonomous</dt><dd>${task.autonomous ? "yes" : "no"}</dd>
    </dl>
    <div class="lbl">task.md</div>
    <pre class="doc">${task.taskMd}</pre>
  </div>`;
}

function TaskView({ id, sessionId, newSession, tick, tasks, onNewSubtask }) {
  const [task, setTask] = useState(null);
  const [error, setError] = useState("");
  const sessions = useSessions(id);
  useEffect(() => {
    get(`/api/tasks/${id}`).then(
      (t) => (setTask(t), setError("")),
      (err) => setError(err.message),
    );
  }, [id, tick]);
  if (error) return html`<div class="empty err">${error}</div>`;
  if (!task) return null;
  const setStatus = (status) => put(`/api/tasks/${task.id}`, { status }).catch((err) => setError(err.message));
  const session = sessions.find((s) => s.id === sessionId);
  return html`<header>
      <span class="t">${task.title}</span>
      <span class="tag mono">${task.branch}</span>
      <span class="tag">${task.workflow}</span>
      ${task.status !== "active" && html`<span class="tag">${task.status}</span>`}
      <div class="actions">
        ${task.status === "active"
          ? html`<button onClick=${() => onNewSubtask(task)}>＋ Sub-task</button><button onClick=${() => setStatus("done")}>Done</button><button onClick=${() => setStatus("archived")}>Archive</button>`
          : html`<button onClick=${() => setStatus("active")}>Reopen</button>`}
      </div>
    </header>
    <nav class="tabs">
      <a class=${!sessionId && !newSession ? "sel" : ""} href=${`#/task/${id}`}>Task</a>
      ${sessions.map(
        (s) => html`<a key=${s.id} class=${s.id === sessionId ? "sel" : ""} href=${`#/task/${id}/s/${s.id}`}>
          <${StatusDot} status=${s.status} />${s.label}
        </a>`,
      )}
      <a class=${newSession ? "sel" : ""} href=${`#/task/${id}/new`} title="New session">＋ Session</a>
    </nav>
    ${newSession
      ? html`<${NewSession} task=${task} onStarted=${(s) => go(id, `s/${s.id}`)} />`
      : session
        ? html`<${SessionView} key=${session.id} session=${session} />`
        : html`<${TaskDetails} task=${task} parent=${tasks.find((t) => t.id === task.parentId)} />`}`;
}

function App() {
  const { projects, tasks, signedIn, tick } = useLive();
  const sessions = useActiveSessions();
  const [where, setWhere] = useState(route());
  const [newTask, setNewTask] = useState(null);
  const [wide, setWide] = useState(localStorage.getItem("panel") === "wide");
  const toggleWide = () => (localStorage.setItem("panel", wide ? "" : "wide"), setWide(!wide));
  useEffect(() => {
    const onHash = () => setWhere(route());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  if (!signedIn) {
    return html`<div class="empty">
      <div class="lbl">Not signed in</div>
      <div>Open the UI with <code>make dev</code>, or visit <code>/auth?t=&lt;token&gt;</code> with the token from <code>~/.layer8/token</code>.</div>
    </div>`;
  }
  return html`<div class=${`app ${where.task ? "with-panel" : ""} ${wide ? "wide" : ""}`}>
    <${Sidebar}
      projects=${projects}
      tasks=${tasks}
      sessions=${sessions}
      selected=${where}
      onSelect=${(id, sessionId) => go(id, sessionId ? `s/${sessionId}` : "")}
      onNewTask=${(project) => setNewTask({ project })}
    />
    <main>
      ${where.task
        ? html`<${TaskView}
            key=${where.task}
            id=${where.task}
            sessionId=${where.session}
            newSession=${where.newSession}
            tick=${tick}
            tasks=${tasks}
            onNewSubtask=${(parent) => setNewTask({ project: projects.find((p) => p.id === parent.projectId), parent })}
          />`
        : html`<div class="empty"><div class="lbl">No task selected</div><div>Pick a task, or press ＋ next to a project to start one.</div></div>`}
    </main>
    ${where.task && html`<${RightPanel} taskId=${where.task} wide=${wide} onWide=${toggleWide} />`}
    ${newTask &&
    html`<${NewTask}
      project=${newTask.project}
      parent=${newTask.parent}
      onClose=${() => setNewTask(null)}
      onCreated=${(t) => (setNewTask(null), go(t.id, t.session ? `s/${t.session.id}` : "new"))}
    />`}
  </div>`;
}

render(html`<${App} />`, document.getElementById("root"));
