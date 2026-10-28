import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { post } from "./api.js";
import { useKeys, cls, prSquare, statusLabel } from "./util.js";
import { SessionsPane, sessionDefaults } from "./sessions.js";
import { ChangesPane } from "./changes.js";
import { PRPane } from "./pr.js";
import { DocsPane } from "./docs.js";
import { Side } from "./side.js";

const TABS = [
  ["sessions", "Sessions"],
  ["changes", "Changes"],
  ["pr", "Pull request"],
  ["docs", "Docs"],
];

export function TaskView({ state, task, run }) {
  const slices = state.slices.filter((x) => x.taskId === task.id);
  const [ctx, setCtx] = useState(""); // "" = the task itself, else a slice id
  const [tab, setTab] = useState("sessions");
  const [sessionId, setSessionId] = useState(null);
  const slice = slices.find((x) => x.id === ctx) || null;
  const sessions = state.sessions.filter((s) => s.taskId === task.id && s.sliceId === (slice ? slice.id : ""));
  const session = sessions.find((s) => s.id === sessionId) || null;

  // Follow the newest session of the current context unless the user picked one.
  useEffect(() => {
    if (!session && sessionId !== "__new" && sessions.length) setSessionId(sessions[sessions.length - 1].id);
  }, [sessions.length, ctx]);
  useEffect(() => {
    if (ctx && !slices.some((x) => x.id === ctx)) setCtx("");
  }, [slices.length]);

  const isGit = !!task.branch;
  const start = async (kind, prompt = "", settings = sessionDefaults()) => {
    const s = await run(post("/api/sessions", { taskId: task.id, sliceId: slice ? slice.id : "", kind, prompt, settings }));
    if (s) {
      setSessionId(s.id);
      setTab("sessions");
    }
  };
  const open = (target) => run(post("/api/open", { taskId: task.id, sliceId: slice ? slice.id : "", target }));
  const where = slice || task;
  const syncNote = where.syncState && where.syncState !== "ok" ? `${where.syncState}${where.syncMessage ? ": " + where.syncMessage : ""}` : where.syncMessage;

  return html`
    <div style="display:grid;grid-template-columns:minmax(0,1fr) 300px;flex:1;min-height:0">
      <div class="pane" style="min-width:0">
        <div class="tabs" role="tablist">
          ${TABS.map(([id, label]) => {
            const n = id === "sessions" ? sessions.length : id === "pr" && slice && slice.openThreads ? slice.openThreads : null;
            return html`<button key=${id} class="tab" role="tab" aria-selected=${tab === id} onClick=${() => setTab(id)}>
              ${label}${n ? html` <span class="n">(${n})</span>` : ""}
            </button>`;
          })}
          <span class="grow"></span>
          <button class="tool" title="Open a terminal in this worktree (T)" onClick=${() => open("terminal")}>${">_"}</button>
          <button class="tool" title="Open the worktree in your editor (E)" onClick=${() => open("editor")}>${"{ }"}</button>
          ${isGit && html`<button class="tool" title="Fetch and rebase this task's branches onto the latest default branch" onClick=${() => run(post(`/api/tasks/${task.id}/sync`))}>⟳</button>`}
        </div>

        ${slices.length > 0 && html`<${StackStrip} task=${task} slices=${slices} state=${state} ctx=${ctx} onPick=${(id) => { setCtx(id); setSessionId(null); }} />`}

        ${tab === "sessions" && html`<${SessionsPane} state=${state} task=${task} slice=${slice} sessions=${sessions} session=${session} onSelect=${setSessionId} run=${run} onStart=${start} />`}
        ${tab === "changes" && (isGit ? html`<${ChangesPane} key=${ctx} task=${task} slice=${slice} state=${state} run=${run} />` : html`<div class="empty">Not a git repo: there is nothing to diff.</div>`)}
        ${tab === "pr" && html`<${PRPane} key=${ctx} task=${task} slice=${slice} slices=${slices} run=${run} onSession=${(s) => { setSessionId(s.id); setTab("sessions"); }} />`}
        ${tab === "docs" && html`<${DocsPane} task=${task} slice=${slice} state=${state} run=${run} />`}
      </div>
      <aside class="side">
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
              <button class="key small" onClick=${() => start("research")}><kbd>R</kbd> Research</button>
              <button class="key small" onClick=${() => start("plan")}><kbd>P</kbd> Plan</button>`}
            <button class="key small" onClick=${() => start("implement")}><kbd>I</kbd> ${slice ? "Implement " + slice.key : "Implement"}</button>
            ${!slice && isGit && html`
              <button class="key small" title="Create one stacked branch + session per PR listed in plan.md" onClick=${() => run(post(`/api/tasks/${task.id}/slices`))}>Split into PRs</button>
              ${!slices.length && html`<button class="key small" title="Track this task's own branch as a single PR" onClick=${() => run(post(`/api/tasks/${task.id}/single-slice`))}>Single PR</button>`}`}
            ${slice && slice.syncState === "conflict" && html`<button class="key small primary" onClick=${async () => { const s = await run(post(`/api/slices/${slice.id}/resolve`)); if (s) { setSessionId(s.id); setTab("sessions"); } }}>Resolve conflicts</button>`}
          </div>
        </div>
        <${Side} state=${state} task=${task} slice=${slice} session=${session} run=${run} onSession=${(s) => { setSessionId(s.id); setTab("sessions"); }} />
      </aside>
    </div>
  `;
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
