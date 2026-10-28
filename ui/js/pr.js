import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { get, post } from "./api.js";
import { cls } from "./util.js";

export function PRPane(props) {
  return props.slice ? html`<${SlicePR} ...${props} />` : html`<${NoSlice} ...${props} />`;
}

function NoSlice({ task, slices }) {
  return html`<div class="empty">
      <div class="display">Pull requests</div>
      ${slices.length
        ? html`Pick a PR in the stack above to see its checks and reviews.`
        : task.branch
        ? html`Write a plan with <b>Plan</b>, then <b>Split into PRs</b> to get one stacked branch and one fresh session per PR. Or use <b>Single PR</b> to ship this task's branch as one pull request.`
        : html`Pull requests need a git repo.`}
    </div>`;
}

function SlicePR({ task, slice, slices, run, onSession }) {
  const [live, setLive] = useState(null);
  const [error, setError] = useState(null);
  const load = async () => {
    if (!slice.prNumber) return;
    try {
      setError(null);
      setLive(await get(`/api/slices/${slice.id}/pr`));
    } catch (e) {
      setError(e.message);
    }
  };
  useEffect(() => {
    load();
  }, [slice.id, slice.prNumber, slice.ciState, slice.openThreads, slice.prState]);

  const act = async (path) => {
    const s = await run(post(`/api/slices/${slice.id}/${path}`));
    if (s && s.id && s.kind) onSession(s);
    load();
  };

  if (!slice.prNumber) {
    const parent = slices.find((x) => x.key === slice.parentKey);
    return html`<div class="prv">
      <div class="display" style="font-size:18px">No pull request yet</div>
      <p class="dim">Pushes <b>${slice.branch}</b> and opens a PR into <b>${parent && parent.prState !== "MERGED" ? parent.branch : (task.baseRef || "").replace(/^origin\//, "")}</b>.
      ${parent && !parent.prNumber && parent.prState !== "MERGED" ? html` Open ${parent.key}'s PR first: this one is stacked on it.` : ""}</p>
      <div class="btns"><button class="key primary" onClick=${() => run(post(`/api/slices/${slice.id}/pr`))}>Push and open PR</button></div>
    </div>`;
  }
  const checks = live ? live.checks : [];
  const failing = checks.filter((c) => ["FAILURE", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE", "ERROR"].includes(c.conclusion));
  const threads = live ? live.openThreads : [];
  return html`<div class="prv">
    <div style="display:flex;gap:12px;align-items:baseline;flex-wrap:wrap">
      <a class="display" style="font-size:18px" href=${slice.prUrl}>#${slice.prNumber}</a>
      <span class="chip"><span class=${cls("sq", slice.prState === "OPEN" ? (slice.prDraft ? "none" : "idle") : slice.prState)}></span>${slice.prDraft ? "draft" : (slice.prState || "").toLowerCase()}</span>
      <span class="chip"><span class=${cls("sq", slice.ciState || "none")}></span>CI ${(slice.ciState || "none").toLowerCase()}</span>
      ${slice.reviewState && html`<span class="chip">${slice.reviewState.replace(/_/g, " ").toLowerCase()}</span>`}
      ${slice.mergeable === "CONFLICTING" && html`<span class="chip bad">conflicts</span>`}
      <span class="faint">into ${slice.prBase}</span>
    </div>
    <div class="btns">
      <button class="key" disabled=${!failing.length} onClick=${() => act("fix-ci")}>Fix CI${failing.length ? ` (${failing.length})` : ""}</button>
      <button class="key" disabled=${!threads.length} onClick=${() => act("review")}>Address review${threads.length ? ` (${threads.length})` : ""}</button>
      <button class="key" onClick=${() => run(post(`/api/slices/${slice.id}/push`))}>Push</button>
      <button class="key ghost" onClick=${() => { run(post("/api/github/refresh")); load(); }}>Refresh</button>
    </div>
    ${error && html`<p class="bad">${error}</p>`}
    <div class="section">
      <div class="label"><span>Checks</span><span>${checks.length}</span></div>
      ${!live && !error && html`<div class="faint">loading…</div>`}
      ${live && !checks.length && html`<div class="faint">No checks reported for the latest commit.</div>`}
      ${checks.map((c) => html`<div class="check" key=${c.name + c.url}>
        <span class=${cls("sq", c.conclusion || (c.status === "COMPLETED" ? "idle" : "PENDING"))}></span>
        <a class="ellipsis" href=${c.url}>${c.name}</a>
        <span class="faint">${(c.conclusion || c.status || "").toLowerCase()}</span>
      </div>`)}
    </div>
    <div class="section">
      <div class="label"><span>Unresolved review threads</span><span>${threads.length}</span></div>
      ${live && !threads.length && html`<div class="faint">None.</div>`}
      ${threads.map((t, i) => html`<div class="thread" key=${i}>
        <div class="faint">${t.path || "general"}${t.line ? ":" + t.line : ""}${t.outdated ? " (outdated)" : ""}</div>
        ${t.comments.map((c, j) => html`<div class="c selectable" key=${j}><b>${c.author}</b> <a href=${c.url}>↗</a>\n${c.body}</div>`)}
      </div>`)}
    </div>
  </div>`;
}
