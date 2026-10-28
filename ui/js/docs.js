import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { get } from "./api.js";
import { home, day, cls } from "./util.js";
import { LineComments } from "./comments.js";

export function DocsPane({ task, slice, state, run }) {
  const [list, setList] = useState([]);
  const [name, setName] = useState(null);
  const [doc, setDoc] = useState(null);
  const [mode, setMode] = useState("read");
  const [comments, setComments] = useState([]);
  const [editing, setEditing] = useState(null);
  const sessionsKey = state.sessions.filter((s) => s.taskId === task.id).map((s) => s.status).join();

  useEffect(() => {
    get(`/api/tasks/${task.id}/artifacts`).then(setList, () => setList([]));
  }, [task.id, sessionsKey]);
  useEffect(() => {
    if (!name && list.length) setName((list.find((a) => a.name === "plan.md") || list[0]).name);
  }, [list]);
  useEffect(() => {
    if (!name) return;
    get(`/api/tasks/${task.id}/artifacts/${encodeURIComponent(name)}`).then(setDoc, (e) => setDoc({ content: "Error: " + e.message }));
  }, [task.id, name, sessionsKey]);
  const loadComments = () =>
    get(`/api/tasks/${task.id}/comments`).then((cs) => setComments(cs.filter((c) => c.target === "artifact")), () => {});
  useEffect(() => {
    loadComments();
  }, [task.id, state.sessions.length]);
  const runC = async (p) => {
    const r = await run(p);
    loadComments();
    return r;
  };

  if (!list.length) return html`<div class="empty">No docs yet. Research and plan sessions write them to <span class="selectable">${home(task.artifactsPath)}</span>.</div>`;
  const lines = doc ? doc.content.split("\n") : [];
  const mine = comments.filter((c) => c.path === name);
  return html`<div class="split">
    <div class="list">
      <div class="list-head label">Docs</div>
      ${list.map((a) => html`<button key=${a.name} class="list-row" aria-selected=${a.name === name} onClick=${() => setName(a.name)}>
        <span class="grow">${a.name}</span>
        ${comments.some((c) => c.path === a.name && c.status === "open") && html`<span class="signal">●</span>`}
        <span class="faint">${day(a.updatedAt)}</span></button>`)}
    </div>
    <div class="content">
      <div class="tabs subtabs" style="position:sticky;top:0;background:var(--bg);z-index:2">
        <button class="tab" aria-selected=${mode === "read"} onClick=${() => setMode("read")}>Read</button>
        <button class="tab" aria-selected=${mode === "comment"} onClick=${() => setMode("comment")}>Comment by line${mine.filter((c) => c.status === "open").length ? ` (${mine.filter((c) => c.status === "open").length})` : ""}</button>
      </div>
      ${!doc
        ? html`<div class="empty">Loading…</div>`
        : mode === "read" && doc.html
        ? html`<div class="doc"><div class="doc-inner md selectable" dangerouslySetInnerHTML=${{ __html: doc.html }}></div></div>`
        : html`<div style="padding:10px 0 30px">
            ${lines.map((l, i) => {
              const n = i + 1;
              const here = mine.filter((c) => c.line === n);
              return html`<div key=${n}>
                <div class=${cls("src", here.length && "commented")}><span class="n" title="Comment on this line" onClick=${() => { setMode("comment"); setEditing(n); }}>${n}</span><span class="c">${l || " "}</span></div>
                ${(here.length > 0 || editing === n) && html`<${LineComments} inSrc=${true} comments=${here} editing=${editing === n} onDone=${() => setEditing(null)} run=${runC}
                  base=${{ taskId: task.id, sliceId: slice ? slice.id : "", target: "artifact", path: name, line: n, quote: l }} />`}
              </div>`;
            })}
          </div>`}
    </div>
  </div>`;
}
