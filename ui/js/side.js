import { html, useState } from "/vendor/preact-htm.js";
import { post } from "./api.js";

export const home = (p) => (p || "").replace(/^\/Users\/[^/]+/, "~");

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

/** Sidebar lists projects and their tasks, newest first; archived tasks are hidden unless asked for. */
export function Sidebar({ projects, tasks, selected, onSelect, onNewTask }) {
  const [adding, setAdding] = useState(false);
  const [archived, setArchived] = useState(false);
  const visible = tasks.filter((t) => archived || t.status !== "archived");
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
        ${visible
          .filter((t) => t.projectId === p.id)
          .map(
            (t) => html`<div key=${t.id} class=${`row ${t.status} ${t.id === selected ? "sel" : ""}`} onClick=${() => onSelect(t.id)}>
              <span class=${`dot ${t.status}`}></span>
              <span class="title">${t.title}</span>
              <span class="tag">${t.workflow}</span>
            </div>`,
          )}
      </div>`,
    )}
    ${!projects.length && !adding && html`<div class="dim">Add a git repository to start.</div>`}
    <div class="foot">
      <label><input type="checkbox" checked=${archived} onChange=${(e) => setArchived(e.target.checked)} /> Show archived</label>
    </div>
  </aside>`;
}
