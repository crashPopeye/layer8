import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { post, put } from "./api.js";
import { useTaskData, SessionPick, openSession } from "./changes.js";

const STATE = { OPEN: "Open", MERGED: "Merged", CLOSED: "Closed" };
const REVIEW = { APPROVED: "approved", CHANGES_REQUESTED: "changes requested", REVIEW_REQUIRED: "review required" };

function checkMark(state) {
  if (state === "SUCCESS") return html`<span class="ok">✓</span>`;
  if (["PENDING", "QUEUED", "IN_PROGRESS", "EXPECTED"].includes(state)) return html`<span class="warn">●</span>`;
  if (["NEUTRAL", "SKIPPED"].includes(state)) return html`<span class="dim">–</span>`;
  return html`<span class="bad">✕</span>`;
}

const failed = (c) => ["FAILURE", "ERROR", "TIMED_OUT", "CANCELLED", "ACTION_REQUIRED", "STARTUP_FAILURE"].includes(c.state);

/** OpenForm opens the task's pull request; its body defaults to the drafted description. */
function OpenForm({ taskId, info, act }) {
  const [title, setTitle] = useState(info.title);
  const [body, setBody] = useState(info.body);
  const [touched, setTouched] = useState(false);
  const [draft, setDraft] = useState(true);
  const [sessionId, setSessionId] = useState("");
  useEffect(() => void (!touched && setBody(info.body)), [info.body]);
  return html`<form class="pr-form" onSubmit=${(e) => (e.preventDefault(), act(`/api/tasks/${taskId}/pr`, { title, body, draft }))}>
    <div class="lbl">Open pull request</div>
    <div class="field mono">${info.head} → ${info.target}</div>
    <input value=${title} onInput=${(e) => setTitle(e.target.value)} placeholder="Title" />
    <textarea
      value=${body}
      onInput=${(e) => (setBody(e.target.value), setTouched(true))}
      placeholder="Body: Draft body with agent writes .layer8/task/pr-description.md with /l8:describe-pr"
    ></textarea>
    ${info.dirty && html`<div class="warn">Uncommitted changes are not part of the PR; commit them in the Changes tab first.</div>`}
    <div class="bar">
      <label class="check"><input type="checkbox" checked=${draft} onChange=${(e) => setDraft(e.target.checked)} /> Draft</label>
      <span class="spacer"></span>
      <${SessionPick} taskId=${taskId} value=${sessionId} onChange=${setSessionId} />
      <button type="button" onClick=${() => (setTouched(false), act(`/api/tasks/${taskId}/pr/draft`, { sessionId }, true))}>Draft body with agent</button>
      <button class="primary" type="submit" disabled=${!title.trim()}>Create PR</button>
    </div>
  </form>`;
}

/** Status shows an existing pull request: state, review, checks and unresolved threads, with the actions that act on them. */
function Status({ taskId, info, act, reload }) {
  const pr = info.pr;
  const [sessionId, setSessionId] = useState("");
  const threads = pr.threads || [];
  const failing = pr.checks.filter(failed);
  return html`<div class="pr-status">
    <div class="pr-head">
      <b>#${pr.number}</b>
      <span class=${`tag ${pr.draft ? "warn" : ""}`}>${pr.draft ? "Draft" : STATE[pr.state] || pr.state}</span>
      ${pr.reviewDecision && html`<span class="dim">${REVIEW[pr.reviewDecision] || pr.reviewDecision}</span>`}
      ${pr.mergeable === "CONFLICTING" && html`<span class="bad">conflicts</span>`}
      <a class="mono" href=${pr.url} target="_blank" rel="noopener">${pr.url.replace(/^https:\/\//, "")}</a>
    </div>
    <div class="mono dim">${pr.head} → ${pr.base}</div>
    ${info.dirty && html`<div class="warn">The worktree has uncommitted changes; commit them in the Changes tab, then Push latest.</div>`}
    ${pr.state === "MERGED" &&
    info.taskStatus === "active" &&
    html`<div class="merged">Merged. Mark task done? <button class="primary" onClick=${() => put(`/api/tasks/${taskId}`, { status: "done" }).then(reload)}>Mark done</button></div>`}
    <div class="lbl">Checks</div>
    ${pr.checks.length
      ? pr.checks.map(
          (c) => html`<div class="check-row">${checkMark(c.state)} <a href=${c.url} target="_blank" rel="noopener">${c.name}</a></div>`,
        )
      : html`<div class="dim">No checks. CI runs locally with make check.</div>`}
    <div class="lbl">Unresolved threads · ${threads.length}</div>
    ${threads.map(
      (t) => html`<div class="comment">
        <div class="where mono">${t.path}${t.line ? `:${t.line}` : ""}${t.outdated ? " (outdated)" : ""}</div>
        ${t.comments.map((c) => html`<div class="note"><b>${c.author}</b> ${c.body}</div>`)}
      </div>`,
    )}
    <div class="bar">
      <${SessionPick} taskId=${taskId} value=${sessionId} onChange=${setSessionId} />
      <span class="spacer"></span>
      <button disabled=${!info.unpushed} onClick=${() => act(`/api/tasks/${taskId}/pr/push`)}>Push latest</button>
      <button disabled=${!threads.length} onClick=${() => act(`/api/tasks/${taskId}/pr/fix-reviews`, { sessionId }, true)}>Fix review comments</button>
      <button disabled=${!failing.length} onClick=${() => act(`/api/tasks/${taskId}/pr/fix-ci`, { sessionId }, true)}>Fix CI</button>
      ${pr.draft && pr.state === "OPEN" && html`<button onClick=${() => act(`/api/tasks/${taskId}/pr/ready`)}>Mark ready</button>`}
    </div>
  </div>`;
}

/** PR is the panel tab with the task's pull request, refreshed on focus, on pushes and by the daemon's poll. */
export function PR({ taskId }) {
  const { data: info, error, reload } = useTaskData(taskId, `/api/tasks/${taskId}/pr`, ["pr", "artifacts"]);
  const [busy, setBusy] = useState(false);
  const [actError, setActError] = useState("");
  const act = async (path, body = {}, toSession = false) => {
    setBusy(true);
    try {
      const res = await post(path, body);
      setActError("");
      if (toSession) openSession(taskId, res);
      reload();
    } catch (err) {
      setActError(err.message);
    } finally {
      setBusy(false);
    }
  };
  if (!info) return html`<div class="pad ${error ? "err" : "dim"}">${error || "Loading pull request…"}</div>`;
  return html`<div class=${`pr ${busy ? "busy" : ""}`}>
    ${(actError || info.error) && html`<div class="err">${actError || info.error}</div>`}
    ${info.pr ? html`<${Status} taskId=${taskId} info=${info} act=${act} reload=${reload} />` : html`<${OpenForm} taskId=${taskId} info=${info} act=${act} />`}
  </div>`;
}
