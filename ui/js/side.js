import { html, useState } from "/vendor/preact-htm.js";
import { post } from "./api.js";
import { StatusDot } from "./conversation.js";

export const home = (p) => (p || "").replace(/^\/Users\/[^/]+/, "~");

const maxIndent = 2;

function AddProject({ onDone }) {
  const [path, setPath] = useState("");
  const [error, setError] = useState("");
  const add = async (p) => {
    try {
      await post("/api/projects", { path: p });
      onDone();
    } catch (err) {
      setError(err.message);
    }
  };
  const submit = (e) => {
    e.preventDefault();
    add(path);
  };
  const choose = async () => {
    try {
      const picked = await post("/api/pick-folder");
      if (picked.path) add(picked.path);
    } catch (err) {
      setError(err.message);
    }
  };
  return html`<form class="add" onSubmit=${submit}>
      <input class="mono" autofocus placeholder="~/Work/repo" value=${path} onInput=${(e) => setPath(e.target.value)} onKeyDown=${(e) => e.key === "Escape" && onDone()} />
      <button type="button" onClick=${choose}>Choose…</button>
      <button class="primary" type="submit">Add</button>
    </form>
    ${error && html`<div class="err">${error}</div>`}`;
}

/** TaskTree renders a task, its sessions when selected, and its sub-tasks. Levels deeper than
 * maxIndent stay at that indent and show the ancestors past it as a breadcrumb. */
function TaskTree({ task, depth, trail, children, sessions, selected, onSelect }) {
  const own = sessions.filter((s) => s.taskId === task.id);
  const waiting = own.filter((s) => s.status === "waiting_approval").length;
  const isSel = task.id === selected.task;
  const indent = (d) => ({ paddingLeft: `${6 + Math.min(d, maxIndent) * 14}px` });
  const kids = children.get(task.id) || [];
  return html`<div class=${`row ${task.status} ${isSel && !selected.session ? "sel" : ""}`} style=${indent(depth)} onClick=${() => onSelect(task.id)}>
      <span class=${`dot ${task.status}`}></span>
      <span class="title">${trail.length > 0 && html`<span class="dim">${trail.join(" › ")} › </span>`}${task.title}</span>
      ${waiting > 0 && html`<span class="badge" title=${`${waiting} waiting for approval`}>${waiting}</span>`}
      ${kids.length > 0 && html`<span class="tag" title="sub-tasks">${kids.length}</span>`}
    </div>
    ${isSel &&
    own.map(
      (s) => html`<div key=${s.id} class=${`row sess ${s.id === selected.session ? "sel" : ""}`} style=${indent(depth + 1)} onClick=${() => onSelect(task.id, s.id)}>
        <${StatusDot} status=${s.status} />
        <span class="title">${s.label}</span>
      </div>`,
    )}
    ${kids.map(
      (k) => html`<${TaskTree} key=${k.id} task=${k} depth=${depth + 1} trail=${depth >= maxIndent ? [...trail, task.title] : []}
        children=${children} sessions=${sessions} selected=${selected} onSelect=${onSelect} />`,
    )}`;
}

/** Sidebar lists projects and their task trees, newest first; archived tasks are hidden unless asked for. */
export function Sidebar({ projects, tasks, sessions, selected, onSelect, onNewTask, onSettings }) {
  const [adding, setAdding] = useState(false);
  const [archived, setArchived] = useState(false);
  const visible = tasks.filter((t) => archived || t.status !== "archived");
  const ids = new Set(visible.map((t) => t.id));
  const children = new Map();
  for (const t of visible) {
    if (t.parentId && ids.has(t.parentId)) children.set(t.parentId, [...(children.get(t.parentId) || []), t]);
  }
  const roots = visible.filter((t) => !t.parentId || !ids.has(t.parentId));
  return html`<aside>
    <div class="head">
      <span class="lbl">Projects</span>
      <button class="icon" title="Add project" onClick=${() => setAdding(!adding)}>＋</button>
    </div>
    ${adding && html`<${AddProject} onDone=${() => setAdding(false)} />`}
    ${projects.map(
      (p) => html`<div key=${p.id}>
        <div class="proj">
          <b>${p.name}</b><span class="tag mono" title=${p.path}>${home(p.path)}</span>
          <button class="icon" title="New task" onClick=${() => onNewTask(p)}>＋</button>
        </div>
        ${roots
          .filter((t) => t.projectId === p.id)
          .map(
            (t) => html`<${TaskTree} key=${t.id} task=${t} depth=${0} trail=${[]} children=${children} sessions=${sessions} selected=${selected} onSelect=${onSelect} />`,
          )}
      </div>`,
    )}
    ${!projects.length && !adding && html`<div class="dim">Add a git repository to start.</div>`}
    <div class="foot">
      <label><input type="checkbox" checked=${archived} onChange=${(e) => setArchived(e.target.checked)} /> Show archived</label>
      <button class="icon" title="Linear key and write-back" onClick=${onSettings}>Settings</button>
    </div>
  </aside>`;
}
