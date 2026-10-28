import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { post } from "./api.js";
import { home, cls, prSquare, statusLabel } from "./util.js";
import { SessionsPane } from "./sessions.js";
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

export function TaskView({ state, task, run, commands }) {
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
  const start = async (kind, prompt = "") => {
    const s = await run(post("/api/sessions", { taskId: task.id, sliceId: slice ? slice.id : "", kind, prompt }));
    if (s) {
      setSessionId(s.id);
      setTab("sessions");
    }
  };
  const open = (target) => run(post("/api/open", { taskId: task.id, sliceId: slice ? slice.id : "", target }));
  const where = slice || task;
  const syncNote = where.syncState && where.syncState !== "ok" ? `${where.syncState}${where.syncMessage ? ": " + where.syncMessage : ""}` : where.syncMessage;
  const resolve = async () => {
    const s = await run(post(`/api/slices/${slice.id}/resolve`));
    if (s) {
      setSessionId(s.id);
      setTab("sessions");
    }
  };

  const actionGroups = [
    [
      !slice && { label: "Research", run: () => start("research") },
      !slice && { label: "Plan", run: () => start("plan") },
      { label: slice ? "Implement " + slice.key : "Implement", run: () => start("implement") },
    ],
    !slice && isGit ? [
      { label: "Split into PRs", title: "Create one stacked branch + session per PR listed in plan.md", run: () => run(post(`/api/tasks/${task.id}/slices`)) },
      !slices.length && { label: "Single PR", title: "Track this task's own branch as a single PR", run: () => run(post(`/api/tasks/${task.id}/single-slice`)) },
    ] : [],
    [
      { label: "Terminal", run: () => open("terminal") },
      { label: "Editor", run: () => open("editor") },
      isGit && { label: "Sync", title: "Fetch and rebase this task's branches onto the latest default branch", run: () => run(post(`/api/tasks/${task.id}/sync`)) },
      slice && slice.syncState === "conflict" && { label: "Resolve conflicts", primary: true, run: resolve },
    ],
  ].map((g) => g.filter(Boolean)).filter((g) => g.length);

  const pick = (id) => {
    setCtx(id);
    setSessionId(null);
  };
  commands.current = [
    ...actionGroups.flat().map((a) => ({ group: "Action", label: a.label, run: a.run })),
    ...TABS.map(([id, label]) => ({ group: "Tab", label, run: () => setTab(id) })),
    ...(slices.length ? [{ group: "Stack", label: "Task: research & plan", run: () => pick("") }] : []),
    ...slices.map((x) => ({ group: "Stack", label: `${x.key}: ${x.title}`, run: () => pick(x.id) })),
  ];
  useEffect(() => () => (commands.current = []), []);

  return html`
    <div style="display:grid;grid-template-columns:minmax(0,1fr) 300px;flex:1;min-height:0">
      <div class="pane" style="min-width:0">
        <div class="task-head">
          <div class="display">
            ${task.linear ? html`<a class="signal" href=${task.linear.url}>${task.linear.identifier}</a>` : html`<span class="signal">Task</span>`}
            ${slice && html`<span class="dim" style="font-size:18px">/ ${slice.key}</span>`}
          </div>
          <h1 class="selectable">${slice ? slice.title : task.title}</h1>
          <div class="meta">
            ${where.branch && html`<span><span class="faint">branch</span><span class="selectable">${where.branch}</span></span>`}
            ${isGit && html`<span><span class="faint">onto</span>${slice ? stackBase(slices, slice, task) : task.baseRef}</span>`}
            <span class="ellipsis"><span class="faint">tree</span><span class="selectable">${home(where.worktreePath)}</span></span>
            ${syncNote && html`<span class=${["conflict", "error", "diverged"].includes(where.syncState) ? "bad" : "dim"}>${syncNote}</span>`}
          </div>
        </div>

        <div class="actions">
          ${actionGroups.map((g, i) => html`
            ${i > 0 && html`<span class="sep"></span>`}
            ${g.map((a) => html`<button key=${a.label} class=${cls("key", a.primary && "primary")} title=${a.title} onClick=${a.run}>${a.label}</button>`)}`)}
        </div>

        ${slices.length > 0 && html`<${StackStrip} task=${task} slices=${slices} state=${state} ctx=${ctx} onPick=${pick} />`}

        <div class="tabs" role="tablist">
          ${TABS.map(([id, label]) => {
            const n = id === "sessions" ? sessions.length : id === "pr" && slice && slice.openThreads ? slice.openThreads : null;
            return html`<button key=${id} class="tab" role="tab" aria-selected=${tab === id} onClick=${() => setTab(id)}>
              ${label}${n ? html` <span class="n">(${n})</span>` : ""}
            </button>`;
          })}
        </div>

        ${tab === "sessions" && html`<${SessionsPane} state=${state} task=${task} slice=${slice} sessions=${sessions} session=${session} onSelect=${setSessionId} run=${run} onStart=${start} />`}
        ${tab === "changes" && (isGit ? html`<${ChangesPane} key=${ctx} task=${task} slice=${slice} state=${state} run=${run} />` : html`<div class="empty">Not a git repo: there is nothing to diff.</div>`)}
        ${tab === "pr" && html`<${PRPane} key=${ctx} task=${task} slice=${slice} slices=${slices} run=${run} onSession=${(s) => { setSessionId(s.id); setTab("sessions"); }} />`}
        ${tab === "docs" && html`<${DocsPane} task=${task} slice=${slice} state=${state} run=${run} />`}
      </div>
      <aside class="side">
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
