import { html, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post } from "./api.js";
import { useEvents, useSessions } from "./events.js";
import { useTaskList, CommentForm, Comments } from "./artifacts.js";

/** useTaskData fetches path and refetches it, debounced, on the task's events of the given kinds and on window focus. */
export function useTaskData(taskId, path, kinds) {
  const [state, setState] = useState({ data: null, error: "" });
  const timer = useRef(0);
  const load = useCallback(
    () => get(path).then((data) => setState({ data, error: "" }), (err) => setState((s) => ({ ...s, error: err.message }))),
    [path],
  );
  const soon = useCallback(() => {
    clearTimeout(timer.current);
    timer.current = setTimeout(load, 300);
  }, [load]);
  useEffect(() => {
    load();
    window.addEventListener("focus", soon);
    return () => (window.removeEventListener("focus", soon), clearTimeout(timer.current));
  }, [load, soon]);
  useEvents((e) => (e.kind === "open" || (kinds.includes(e.kind) && e.id === taskId)) && soon());
  return { ...state, reload: load };
}

/** useChanges is the task's diff against its merge-base, kept fresh as agents edit. */
export const useChanges = (taskId) => useTaskData(taskId, `/api/tasks/${taskId}/changes`, ["changes"]);

/** SessionPick chooses which of the task's sessions receives a message; "" starts a new one. */
export function SessionPick({ taskId, value, onChange }) {
  const sessions = useSessions(taskId);
  return html`<select class="pick" value=${value} onChange=${(e) => onChange(e.target.value)} title="Session that receives the request">
    <option value="">new session</option>
    ${sessions.map((s) => html`<option value=${s.id}>${s.label}</option>`)}
  </select>`;
}

export const openSession = (taskId, s) => (location.hash = `#/task/${taskId}/s/${s.id}`);

const filePath = (f) => (f.status === "deleted" ? f.oldPath : f.newPath);

/** splitRows pairs each run of deleted lines with the added lines that follow it, for the side-by-side view. */
function splitRows(lines) {
  const rows = [];
  for (let i = 0; i < lines.length; ) {
    if (lines[i].kind === "ctx") {
      rows.push([lines[i], lines[i]]);
      i++;
      continue;
    }
    const dels = [];
    const adds = [];
    while (i < lines.length && lines[i].kind === "del") dels.push(lines[i++]);
    while (i < lines.length && lines[i].kind === "add") adds.push(lines[i++]);
    for (let j = 0; j < Math.max(dels.length, adds.length); j++) rows.push([dels[j], adds[j]]);
  }
  return rows;
}

/** Cell is one side of a diff line, after an optional extra gutter number; clicking its number starts a comment on it. */
function Cell({ line, gutter, n, marked, onComment }) {
  if (!line) return html`<div class="dl none"><span class="ln"></span><span class="code"></span></div>`;
  return html`<div class=${`dl ${line.kind} ${marked ? "commented" : ""}`}>
    ${gutter !== undefined && html`<span class="ln old">${gutter || ""}</span>`}
    <span class="ln" title="Comment on this line" onClick=${() => onComment(line)}>${n || ""}</span>
    <span class="code">${line.kind === "add" ? "+" : line.kind === "del" ? "−" : " "}${line.text}</span>
  </div>`;
}

/** FileDiff shows one file's hunks, unified or split, with commented lines marked. */
function FileDiff({ file, mode, commented, onComment }) {
  if (file.binary) return html`<div class="dim pad">Binary or large file; not shown.</div>`;
  if (!file.hunks.length) return html`<div class="dim pad">${file.status === "renamed" ? "Renamed without changes." : "Empty file."}</div>`;
  const marked = (l) => commented.has(l.new || l.old);
  return html`<div class=${`diff mono ${mode}`}>
    ${file.hunks.map(
      (h) => html`<div class="hunk">
        <div class="hh">${h.header}</div>
        ${mode === "split"
          ? splitRows(h.lines).map(
              ([a, b]) => html`<div class="row2">
                <${Cell} line=${a} n=${a && a.old} marked=${a && a.kind === "del" && marked(a)} onComment=${onComment} />
                <${Cell} line=${b} n=${b && b.new} marked=${b && b.kind !== "del" && marked(b)} onComment=${onComment} />
              </div>`,
            )
          : h.lines.map((l) => html`<${Cell} line=${l} gutter=${l.old || 0} n=${l.new} marked=${marked(l)} onComment=${onComment} />`)}
      </div>`,
    )}
  </div>`;
}

/** CommitDialog commits every change in the worktree; an agent can draft the message into the artifacts folder. */
function CommitDialog({ taskId, onClose }) {
  const [message, setMessage] = useState("");
  const [sessionId, setSessionId] = useState("");
  const [drafting, setDrafting] = useState(false);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const loadDraft = useCallback(
    () =>
      fetch(`/api/tasks/${taskId}/artifacts/commit-message.txt`)
        .then((r) => (r.ok ? r.text() : ""))
        .then((t) => t.trim() && (setMessage(t.trim()), setDrafting(false))),
    [taskId],
  );
  useEffect(() => void loadDraft(), [loadDraft]);
  useEvents((e) => drafting && e.kind === "artifacts" && e.id === taskId && loadDraft());
  const draft = () =>
    post(`/api/tasks/${taskId}/commit/draft`, { sessionId }).then(
      () => (setDrafting(true), setError("")),
      (err) => setError(err.message),
    );
  const commit = async (e) => {
    e.preventDefault();
    setBusy(true);
    try {
      await post(`/api/tasks/${taskId}/commit`, { message });
      onClose();
    } catch (err) {
      setError(err.message);
    } finally {
      setBusy(false);
    }
  };
  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <form class="dialog" onSubmit=${commit}>
      <div class="lbl">Commit all changes in the worktree</div>
      <textarea
        autofocus
        placeholder=${drafting ? "The agent is writing .layer8/task/commit-message.txt…" : "Summary line, blank line, then why"}
        value=${message}
        onInput=${(e) => setMessage(e.target.value)}
        onKeyDown=${(e) => (e.key === "Enter" && (e.metaKey || e.ctrlKey) && commit(e)) || (e.key === "Escape" && onClose())}
      ></textarea>
      <div class="btns">
        ${error && html`<span class="err">${error}</span>`}
        <${SessionPick} taskId=${taskId} value=${sessionId} onChange=${setSessionId} />
        <button type="button" disabled=${drafting} onClick=${draft}>${drafting ? "Drafting…" : "Draft with agent"}</button>
        <button type="button" onClick=${onClose}>Cancel</button>
        <button class="primary" type="submit" disabled=${busy || !message.trim()}>Commit</button>
      </div>
    </form>
  </div>`;
}

/** Changes is the panel tab with the task's diff against its merge-base, line comments and Commit…. */
export function Changes({ taskId, changes }) {
  const comments = useTaskList(taskId, `/api/tasks/${taskId}/comments`, "comments");
  const [mode, setMode] = useState(localStorage.getItem("diffMode") || "unified");
  const [selected, setSelected] = useState("");
  const [writing, setWriting] = useState(null);
  const [committing, setCommitting] = useState(false);
  const { data, error, reload } = changes;
  const files = data ? data.files : [];
  const current = files.find((f) => filePath(f) === selected) || files[0];
  const diffComments = comments.filter((c) => c.target === "diff" && c.status === "open");
  const counts = {};
  for (const c of diffComments) counts[c.path] = (counts[c.path] || 0) + 1;
  const pickMode = (m) => (localStorage.setItem("diffMode", m), setMode(m));
  return html`<div class="changes">
    <div class="bar">
      <span class="lbl" title=${data ? data.mergeBase : ""}>${data ? `vs ${data.base} @ ${data.mergeBase.slice(0, 7)} (merge-base)` : ""}</span>
      <span class="spacer"></span>
      <select class="pick" value=${mode} onChange=${(e) => pickMode(e.target.value)}>
        <option value="unified">Unified</option>
        <option value="split">Split</option>
      </select>
      <button class="icon" title="Refresh" onClick=${reload}>⟳</button>
      <button disabled=${!files.length} onClick=${() => setCommitting(true)}>Commit…</button>
    </div>
    ${error && html`<div class="err pad">${error}</div>`}
    <div class="files">
      ${files.map((f) => {
        const p = filePath(f);
        return html`<div key=${p} class=${`file ${f === current ? "sel" : ""}`} onClick=${() => (setSelected(p), setWriting(null))}>
          <span class="name mono" title=${p}>${f.status === "renamed" ? `${f.oldPath} → ${f.newPath}` : p}</span>
          ${f.untracked && html`<span class="lbl">untracked</span>`}
          ${f.status === "deleted" && html`<span class="lbl">deleted</span>`}
          ${counts[p] && html`<span class="tag warn">${counts[p]}</span>`}
          <span class="mono stat"><span class="ok">+${f.adds}</span> <span class="bad">−${f.dels}</span></span>
        </div>`;
      })}
      ${data && !files.length && html`<div class="dim">No changes against ${data.base}.</div>`}
    </div>
    <div class="viewer-body">
      ${current &&
      html`<${FileDiff}
        key=${filePath(current)}
        file=${current}
        mode=${mode}
        commented=${new Set(diffComments.filter((c) => c.path === filePath(current)).map((c) => c.line))}
        onComment=${(l) => setWriting({ line: l.new || l.old, quote: l.text.trim() })}
      />`}
    </div>
    ${writing && current && html`<${CommentForm} taskId=${taskId} target="diff" path=${filePath(current)} at=${writing} onDone=${() => setWriting(null)} />`}
    <${Comments} taskId=${taskId} target="diff" comments=${comments} onOpen=${setSelected} />
    ${committing && html`<${CommitDialog} taskId=${taskId} onClose=${() => (setCommitting(false), reload())} />`}
  </div>`;
}
