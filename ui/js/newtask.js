import { html, useState, useEffect } from "/vendor/preact-htm.js";
import { post } from "./api.js";

const workflows = ["freeform", "oneshot", "outline_only", "prd_tdd", "rpi"];

/** NewTask is the dialog that creates a task in project; it calls onCreated with the new task. */
export function NewTask({ project, onClose, onCreated }) {
  const [form, setForm] = useState({ title: "", workflow: "freeform", prompt: "", autonomous: false });
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
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
      onCreated(await post("/api/tasks", { ...form, projectId: project.id }));
    } catch (err) {
      setError(err.message);
      setBusy(false);
    }
  };
  const submitOnCmdEnter = (e) => e.key === "Enter" && e.metaKey && submit(e);

  return html`<div class="scrim" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <form class="dialog" onSubmit=${submit} onKeyDown=${submitOnCmdEnter}>
      <div class="lbl">New task in ${project.name}</div>
      <label class="field"><span class="lbl">Title</span><input autofocus value=${form.title} onInput=${set("title")} /></label>
      <label class="field"
        ><span class="lbl">Workflow</span>
        <select value=${form.workflow} onChange=${set("workflow")}>
          ${workflows.map((w) => html`<option value=${w}>${w}</option>`)}
        </select>
      </label>
      <label class="field"><span class="lbl">Prompt</span><textarea value=${form.prompt} onInput=${set("prompt")} placeholder="What should be done? Written to task.md."></textarea></label>
      <label class="check"><input type="checkbox" checked=${form.autonomous} onChange=${set("autonomous")} /> Autonomous: agents answer their own questions</label>
      <div class="btns">
        ${error && html`<span class="err">${error}</span>`}
        <button type="button" onClick=${onClose}>Cancel</button>
        <button class="primary" type="submit" disabled=${busy || !form.title.trim()}>${busy ? "Creating…" : "Create ⌘↩"}</button>
      </div>
    </form>
  </div>`;
}
