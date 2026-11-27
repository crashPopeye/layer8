import { html } from "/vendor/preact-htm.js";
import { post } from "./api.js";
import { cls, prSquare, statusLabel } from "./util.js";
import { Side } from "./side.js";
import { Icon } from "./icons.js";

/** The focused task or PR: what it is, where it lives, the PR stack and its actions. */
export function TaskPanel({ state, task, slice, slices, session, run, start, open, showSession, setFocus }) {
  const isGit = !!task.branch;
  const where = slice || task;
  const syncNote = where.syncState && where.syncState !== "ok" ? `${where.syncState}${where.syncMessage ? ": " + where.syncMessage : ""}` : where.syncMessage;
  const resolve = async () => {
    const s = await run(post(`/api/slices/${slice.id}/resolve`));
    if (s) showSession(s);
  };
  return html`<div class="pane task-panel">
    <div class="block task-head">
      <div class="display">
        ${task.linear ? html`<a class="signal" href=${task.linear.url}>${task.linear.identifier}</a>` : html`<span class="signal">Task</span>`}
        ${slice && html`<span class="dim">/ ${slice.key}</span>`}
      </div>
      <h1 class="selectable">${slice ? slice.title : task.title}</h1>
      <dl class="kv">
        ${where.branch && html`<dt>branch</dt><dd class="selectable" title=${where.branch}>${where.branch}</dd>`}
        ${isGit && html`<dt>onto</dt><dd>${slice ? stackBase(slices, slice, task) : task.baseRef}</dd>`}
      </dl>
      ${syncNote && html`<div class=${["conflict", "error", "diverged"].includes(where.syncState) ? "bad" : "dim"} style="margin-top:6px">${syncNote}</div>`}
      <div class="btns">
        ${!slice && html`
          <button class="key small" onClick=${() => start("research")}><${Icon} name="telescope" /> Research</button>
          <button class="key small" onClick=${() => start("plan")}><${Icon} name="list-checks" /> Plan</button>`}
        <button class="key small" onClick=${() => start("implement")}><${Icon} name="hammer" /> ${slice ? "Implement " + slice.key : "Implement"}</button>
        ${!slice && isGit && html`
          <button class="key small" title="Create one stacked branch + session per PR listed in plan.md" onClick=${() => run(post(`/api/tasks/${task.id}/slices`))}><${Icon} name="git-fork" /> Split into PRs</button>
          ${!slices.length && html`<button class="key small" title="Track this task's own branch as a single PR" onClick=${() => run(post(`/api/tasks/${task.id}/single-slice`))}><${Icon} name="git-pull-request" /> Single PR</button>`}`}
        ${slice && slice.syncState === "conflict" && html`<button class="key small primary" onClick=${resolve}><${Icon} name="git-merge" /> Resolve conflicts</button>`}
        <button class="key small ghost" title="Open a terminal in this worktree" onClick=${() => open("terminal")}><${Icon} name="square-terminal" /></button>
        <button class="key small ghost" title="Open the worktree in your editor" onClick=${() => open("editor")}><${Icon} name="code-xml" /></button>
        ${isGit && html`<button class="key small ghost" title="Fetch and rebase this task's branches onto the latest default branch" onClick=${() => run(post(`/api/tasks/${task.id}/sync`))}><${Icon} name="refresh-cw" /></button>`}
      </div>
    </div>
    ${slices.length > 0 && html`<${StackStrip} task=${task} slices=${slices} state=${state} ctx=${slice ? slice.id : ""} onPick=${(id) => setFocus({ taskId: task.id, sliceId: id })} />`}
    <${Side} state=${state} task=${task} slice=${slice} session=${session} run=${run} onSession=${showSession} />
  </div>`;
}

function stackBase(slices, slice, task) {
  let key = slice.parentKey;
  while (key) {
    const p = slices.find((x) => x.key === key);
    if (!p) break;
    if (p.prState !== "MERGED") return p.branch;
    key = p.parentKey;
  }
  return task.baseRef;
}

function StackStrip({ task, slices, state, ctx, onPick }) {
  const busy = (x) => state.sessions.filter((s) => s.sliceId === x.id).slice(-1)[0];
  return html`<div class="stack">
    <button class="slice" aria-selected=${ctx === ""} onClick=${() => onPick("")}>
      <span class="k">Task</span><span class="ellipsis">Research & plan</span>
      <span class="row2">${task.branch || "folder"}</span>
    </button>
    ${slices.map((x) => {
      const last = busy(x);
      return html`<button key=${x.id} class="slice" aria-selected=${ctx === x.id} onClick=${() => onPick(x.id)} title=${x.title}>
        <span class="k">${x.key}</span><span class="ellipsis">${x.title}</span>
        <span class="row2">
          <span class=${cls("sq", last ? last.status : "none")} title=${last ? "agent: " + statusLabel(last.status) : "no session yet"}></span>
          <span class=${cls("sq", prSquare(x))} title=${x.prNumber ? `PR #${x.prNumber} ${x.prState} · CI ${x.ciState || "none"}` : "no PR yet"}></span>
          <span class=${cls("sq", x.syncState || "none")} title=${"sync: " + (x.syncState || "unknown") + (x.syncMessage ? " · " + x.syncMessage : "")}></span>
          ${x.prNumber ? html`<span>#${x.prNumber}</span>` : html`<span>no PR</span>`}
          ${x.openThreads > 0 && html`<span class="waitc">${x.openThreads} open</span>`}
          ${x.reviewState === "APPROVED" && html`<span class="okc">✓</span>`}
        </span>
      </button>`;
    })}
  </div>`;
}
