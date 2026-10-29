import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { get, post, del } from "./api.js";
import { home, usd, statusLabel } from "./util.js";
import { Icon } from "./icons.js";

export function Side({ state, task, slice, session, run, onSession }) {
  const [comments, setComments] = useState([]);
  const sliceId = slice ? slice.id : "";
  const load = () => get(`/api/tasks/${task.id}/comments`).then(setComments, () => setComments([]));
  // Comments are added from the Changes and Docs panes; refresh on any state ping.
  useEffect(() => {
    load();
    const t = setInterval(load, 4000);
    window.addEventListener("layer8:comments", load);
    return () => {
      clearInterval(t);
      window.removeEventListener("layer8:comments", load);
    };
  }, [task.id]);
  const open = comments.filter((c) => c.status === "open" && (c.target === "artifact" || c.sliceId === sliceId));
  const archive = () => {
    if (!confirm("Archive this task? Branches are kept.")) return;
    const removeWorktrees = !!task.branch && confirm("Also remove its worktrees? (OK = remove, Cancel = keep them on disk)");
    run(post(`/api/tasks/${task.id}/archive`, { removeWorktrees }));
  };
  return html`
    <div class="block">
      <div class="label"><span>Comments for the agent</span><span>${open.length}</span></div>
      ${!open.length && html`<div class="faint">Select text in Docs or Changes and press <kbd>C</kbd> to comment. Comments stack here and go to the agent together, in one message.</div>`}
      ${open.map((c) => html`<div class="q" key=${c.id}>
        <div class="faint ellipsis">${c.path}${c.line ? ":" + c.line : ""}</div>
        ${c.quote && html`<div class="quote ellipsis">${c.quote.replace(/^[+ -]/, "")}</div>`}
        <div class="body">${c.body}</div>
        <button class="x" title="Delete comment" onClick=${async () => { await run(del(`/api/comments/${c.id}`)); load(); }}><${Icon} name="x" /></button>
      </div>`)}
      ${open.length > 0 && html`<div class="btns">
        <button class="key primary" onClick=${async () => {
          const s = await run(post("/api/comments/send", { taskId: task.id, sliceId, sessionId: session && !["queued", "running", "waiting_approval"].includes(session.status) ? session.id : "" }));
          if (s && s.id) onSession(s);
          load();
        }}>Send ${open.length} to ${session ? "this session" : "a new session"}</button>
      </div>`}
    </div>

    ${session && html`<div class="block">
      <div class="label"><span>Session</span><span class="dim">${statusLabel(session.status)}</span></div>
      <dl class="kv">
        <dt>workflow</dt><dd>${session.kind}</dd>
        <dt>cost</dt><dd>${usd(session.costUsd)}</dd>
        <dt>claude id</dt><dd class="selectable" title=${session.claudeSessionId}>${session.claudeSessionId ? session.claudeSessionId.slice(0, 8) : "—"}</dd>
      </dl>
      ${session.lastError && html`<div class="bad selectable" style="margin-top:6px;white-space:pre-wrap;max-height:120px;overflow:auto">${session.lastError}</div>`}
      <div class="btns">
        <button class="key" disabled=${!session.claudeSessionId} title="claude --resume in this worktree's tmux session" onClick=${() => run(post(`/api/sessions/${session.id}/resume-terminal`))}><${Icon} name="square-terminal" /> Continue in terminal</button>
      </div>
    </div>`}

    ${slice && html`<div class="block">
      <div class="label"><span>PR ${slice.key}</span><span class="dim">${slice.syncState || ""}</span></div>
      <dl class="kv">
        ${slice.linear && html`<dt>linear</dt><dd><a href=${slice.linear.url}>${slice.linear.identifier} ↗</a></dd>`}
        <dt>branch</dt><dd class="selectable" title=${slice.branch}>${slice.branch}</dd>
        <dt>stacked on</dt><dd>${slice.parentKey || "default branch"}</dd>
        <dt>tmux</dt><dd>${slice.tmuxSession || "off"}</dd>
      </dl>
      ${slice.syncMessage && html`<div class="dim" style="margin-top:6px">${slice.syncMessage}</div>`}
    </div>`}

    <div class="block">
      <div class="label">Task</div>
      <dl class="kv">
        ${task.linear && html`<dt>linear</dt><dd><a href=${task.linear.url}>${task.linear.identifier} ↗</a></dd>`}
        <dt>docs</dt><dd class="selectable" title=${task.artifactsPath}>${home(task.artifactsPath)}</dd>
        <dt>tree</dt><dd class="selectable" title=${task.worktreePath}>${home(task.worktreePath)}</dd>
        <dt>cost</dt><dd>${usd(state.sessions.filter((s) => s.taskId === task.id).reduce((n, s) => n + s.costUsd, 0))}</dd>
      </dl>
      <div class="btns"><button class="key danger" onClick=${archive}><${Icon} name="archive" /> Archive</button></div>
    </div>`;
}
