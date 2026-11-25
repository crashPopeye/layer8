import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { get, post } from "./api.js";
import { useLinear } from "./settings.js";

const workflows = ["freeform", "oneshot", "outline_only", "prd_tdd", "rpi"];

/** TicketPicker searches My issues (empty query), an identifier such as FLOW-1297, or any text,
 * and calls onPick with the full issue, children included. */
function TicketPicker({ onPick }) {
  const [q, setQ] = useState("");
  const [issues, setIssues] = useState([]);
  const [error, setError] = useState("");
  useEffect(() => {
    const timer = setTimeout(() => {
      get(`/api/linear/issues?q=${encodeURIComponent(q.trim())}`).then(
        (found) => (setIssues(found), setError("")),
        (err) => setError(err.message),
      );
    }, 250);
    return () => clearTimeout(timer);
  }, [q]);
  const pick = (i) => get(`/api/linear/issues/${encodeURIComponent(i.identifier)}`).then(onPick, (err) => setError(err.message));
  return html`<div class="tickets">
    <input placeholder="My issues · FLOW-1297 · search text" value=${q} onInput=${(e) => setQ(e.target.value)} />
    ${error && html`<div class="err">${error}</div>`}
    <div class="list">
      ${issues.slice(0, 8).map(
        (i) => html`<button type="button" key=${i.id} class="issue" onClick=${() => pick(i)}>
          <span class="mono">${i.identifier}</span><span class="title">${i.title}</span><span class="dim">${i.state}</span>
        </button>`,
      )}
      ${!error && issues.length === 0 && html`<span class="dim">No issues.</span>`}
    </div>
  </div>`;
}

/** NewTask is the dialog that creates a task in project, or a sub-task of parent; it calls
 * onCreated with the new task and, when started, its first session. */
export function NewTask({ project, parent, onClose, onCreated }) {
  const [form, setForm] = useState({ title: "", workflow: "freeform", prompt: "", autonomous: !!parent?.autonomous, autoAdvance: false, start: false, children: false });
  const [ticket, setTicket] = useState(null);
  const [picking, setPicking] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const linear = useLinear();
  const set = (k) => (e) => setForm({ ...form, [k]: e.target.type === "checkbox" ? e.target.checked : e.target.value });

  useEffect(() => {
    const onKey = (e) => e.key === "Escape" && onClose();
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const submit = async (e) => {
    e.preventDefault();
    setBusy(true);
    setError("");
    try {
      const children = !!ticket?.children?.length && form.children;
      onCreated(await post("/api/tasks", { ...form, children, ticket: ticket?.identifier || "", projectId: project.id, parentId: parent?.id || "" }));
    } catch (err) {
      setError(err.message);
      setBusy(false);
    }
  };
  const submitOnCmdEnter = (e) => e.key === "Enter" && e.metaKey && submit(e);
  const pick = (issue) => (setTicket(issue), setPicking(false));

  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <form class="dialog" onSubmit=${submit} onKeyDown=${submitOnCmdEnter}>
      <div class="lbl">${parent ? html`New sub-task of ${parent.title}` : html`New task in ${project.name}`}</div>
      <div class="field">
        <span class="lbl">Linear ticket</span>
        ${ticket
          ? html`<div class="row-inline">
              <span class="tag mono">${ticket.identifier}</span><span class="title">${ticket.title}</span>
              <button type="button" class="icon" title="Unlink" onClick=${() => setTicket(null)}>×</button>
            </div>
            <span class="dim mono">${ticket.branchName}</span>`
          : picking
            ? html`<${TicketPicker} onPick=${pick} />`
            : linear
              ? html`<button type="button" onClick=${() => setPicking(true)}>Link a ticket…</button>`
              : linear === false && html`<span class="dim">Paste a Linear API key in Settings to link tickets.</span>`}
      </div>
      <label class="field"><span class="lbl">Title</span><input autofocus value=${form.title} placeholder=${ticket?.title || ""} onInput=${set("title")} /></label>
      <label class="field"
        ><span class="lbl">Workflow</span>
        <select value=${form.workflow} onChange=${set("workflow")}>
          ${workflows.map((w) => html`<option value=${w}>${w}</option>`)}
        </select>
      </label>
      <label class="field"><span class="lbl">Prompt</span><textarea value=${form.prompt} onInput=${set("prompt")} placeholder="What should be done? Written to task.md."></textarea></label>
      <label class="check"><input type="checkbox" checked=${form.autonomous} onChange=${set("autonomous")} /> Autonomous: agents answer their own questions</label>
      ${form.workflow !== "freeform" &&
      html`<label class="check"><input type="checkbox" checked=${form.autoAdvance} onChange=${set("autoAdvance")} /> Auto-advance: start each next phase as soon as the previous one is done</label>`}
      ${ticket?.children?.length > 0 &&
      html`<label class="check"
        ><input type="checkbox" checked=${form.children} onChange=${set("children")} /> Create sub-tasks from children (${ticket.children.map((c) => c.identifier).join(", ")})</label
      >`}
      <label class="check"
        ><input type="checkbox" checked=${form.start} onChange=${set("start")} /> ${form.workflow === "freeform" ? "Start a session with the prompt" : "Start the first phase"}</label
      >
      <div class="btns">
        ${error && html`<span class="err">${error}</span>`}
        <button type="button" onClick=${onClose}>Cancel</button>
        <button class="primary" type="submit" disabled=${busy || (!form.title.trim() && !ticket)}>${busy ? "Creating…" : "Create ⌘↩"}</button>
      </div>
    </form>
  </div>`;
}
