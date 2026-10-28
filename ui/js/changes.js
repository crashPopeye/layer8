import { html, useState, useEffect, useCallback } from "/vendor/preact-htm.js";
import { get, post, withQuery } from "./api.js";
import { cls, day } from "./util.js";
import { LineComments } from "./comments.js";

export function ChangesPane({ task, slice, state, run }) {
  const [sha, setSha] = useState("");
  const [data, setData] = useState(null);
  const [error, setError] = useState(null);
  const [comments, setComments] = useState([]);
  const [message, setMessage] = useState("");
  const sliceId = slice ? slice.id : "";

  const load = useCallback(async () => {
    try {
      setError(null);
      setData(await get(withQuery("/api/diff", { task: task.id, slice: sliceId, sha })));
    } catch (e) {
      setError(e.message);
    }
  }, [task.id, sliceId, sha]);
  const loadComments = useCallback(async () => {
    try {
      setComments((await get(`/api/tasks/${task.id}/comments`)).filter((c) => c.target === "diff" && c.sliceId === sliceId));
    } catch {
      setComments([]);
    }
  }, [task.id, sliceId]);

  // Reload when sessions in this worktree change state (an agent may have committed).
  const sessionsKey = state.sessions.filter((s) => s.taskId === task.id).map((s) => s.status).join();
  useEffect(() => {
    load();
  }, [load, sessionsKey]);
  useEffect(() => {
    loadComments();
  }, [loadComments, state.sessions.length]);

  const runC = async (p) => {
    const r = await run(p);
    loadComments();
    return r;
  };
  const commit = async () => {
    const r = await run(post("/api/commit", { taskId: task.id, sliceId, message }));
    if (r) {
      setMessage("");
      load();
    }
  };

  if (error) return html`<div class="empty bad">${error}</div>`;
  if (!data) return html`<div class="empty">Loading changes…</div>`;
  const dirty = !sha && data.dirty;

  return html`<div class="split">
    <div class="list">
      <div class="list-head label">Commits onto ${data.base}</div>
      <button class="list-row" aria-selected=${sha === ""} onClick=${() => setSha("")}>
        <span class="grow">All changes${!sha && data.untracked && data.untracked.length ? ` (+${data.untracked.length} new)` : ""}</span>
      </button>
      ${data.commits.map((c) => html`<button key=${c.sha} class="list-row" aria-selected=${sha === c.sha} onClick=${() => setSha(c.sha)} title=${c.subject + "\n" + c.author}>
        <span class="faint">${c.short}</span><span class="grow">${c.subject}</span><span class="faint">${day(c.date)}</span>
      </button>`)}
      ${!data.commits.length && html`<div class="empty" style="padding:10px 12px">No commits yet.</div>`}
      <div class="list-head label" style="margin-top:10px">Files</div>
      ${data.files.map((f) => html`<a key=${f.newPath + f.oldPath} class="list-row" href=${"#f-" + encodeURIComponent(f.newPath || f.oldPath)}
          onClick=${(e) => { e.preventDefault(); document.getElementById("f-" + (f.newPath || f.oldPath))?.scrollIntoView(); }}>
        <span class="grow">${f.status === "deleted" ? f.oldPath : f.newPath}</span>
        <span class="okc">+${f.adds}</span><span class="bad">-${f.dels}</span>
      </a>`)}
      ${!sha && (data.untracked || []).map((p) => html`<div key=${p} class="list-row"><span class="grow">${p}</span><span class="waitc">new</span></div>`)}
      ${dirty && html`<div class="commit-box">
        <span class="label">Commit everything here</span>
        <textarea class="field" rows="3" placeholder="Commit message" value=${message} onInput=${(e) => setMessage(e.target.value)}></textarea>
        <button class="key" disabled=${!message.trim()} onClick=${commit}>Commit</button>
      </div>`}
    </div>
    <div class="content">
      ${sha && data.commit && html`<div class="hunk-head selectable" style="padding:10px 14px"><b>${data.commit.subject}</b>${data.commit.body ? "\n\n" + data.commit.body : ""}<div class="faint">${data.commit.author} · ${data.commit.sha}</div></div>`}
      ${!data.files.length && html`<div class="empty">No changes${sha ? " in this commit" : " on this branch yet"}.</div>`}
      ${data.files.map((f) => html`<${FileDiff} key=${f.newPath + f.oldPath + sha} file=${f} comments=${comments} base=${{ taskId: task.id, sliceId, target: "diff" }} run=${runC} />`)}
    </div>
  </div>`;
}

function FileDiff({ file, comments, base, run }) {
  const [editing, setEditing] = useState(null); // line key "new:12" / "old:7"
  const path = file.status === "deleted" ? file.oldPath : file.newPath;
  const mine = comments.filter((c) => c.path === path);
  return html`<div class="file" id=${"f-" + path}>
    <div class="file-head"><span class="path selectable">${file.status === "renamed" ? `${file.oldPath} → ${file.newPath}` : path}</span>
      <span class="chip">${file.status}</span><span class="okc">+${file.adds}</span><span class="bad">-${file.dels}</span></div>
    ${file.binary && html`<div class="hunk-head">Binary file</div>`}
    ${(file.hunks || []).map((h, hi) => html`<div key=${hi}>
      <div class="hunk-head">${h.header}</div>
      ${h.lines.map((l, li) => {
        // Comments anchor to new-side line numbers; deleted lines use the old number.
        const line = l.kind === "del" ? l.old : l.new;
        const key = `${l.kind === "del" ? "old" : "new"}:${line}`;
        const here = mine.filter((c) => c.line === line && (l.kind === "del") === c.quote.startsWith("-"));
        return html`<div key=${li}>
          <div class=${cls("dl", l.kind, here.length && "commented")}>
            <span class="n" title="Comment on this line" onClick=${() => setEditing(key)}>${l.old || ""}</span>
            <span class="n" title="Comment on this line" onClick=${() => setEditing(key)}>${l.new || ""}</span>
            <span class="s">${l.kind === "add" ? "+" : l.kind === "del" ? "-" : " "}</span>
            <span class="c">${l.text}</span>
          </div>
          ${(here.length > 0 || editing === key) && html`<${LineComments} comments=${here} editing=${editing === key} onDone=${() => setEditing(null)} run=${run}
            base=${{ ...base, path, line, quote: (l.kind === "del" ? "-" : l.kind === "add" ? "+" : " ") + l.text }} />`}
        </div>`;
      })}
    </div>`)}
  </div>`;
}
