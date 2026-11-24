import { html, useState } from "/vendor/preact-htm.js";
import { post, put } from "./api.js";

/** PhaseRail shows a task's workflow phases, offers to start the next one, and toggles
 * auto-advance and autonomy; onStarted receives the session a started phase runs in. */
export function PhaseRail({ task, onStarted }) {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  if (!task.phases || !task.phases.length) return null;
  const act = async (fn) => {
    setBusy(true);
    setError("");
    try {
      await fn();
    } catch (err) {
      setError(err.message);
    }
    setBusy(false);
  };
  const start = () => act(async () => onStarted(await post(`/api/tasks/${task.id}/phase/next`)));
  const toggle = (key) => act(() => put(`/api/tasks/${task.id}`, { [key]: !task[key] }));
  return html`<div class="rail">
    ${task.phases.map(
      (p, i) => html`${i > 0 && html`<span class="sep">›</span>`}<span
          class=${`ph ${p.done ? "done" : ""} ${p.name === task.phase ? "cur" : ""}`}
          title=${p.skill || "the task's prompt"}
          >${p.done ? "✓ " : ""}${p.name}</span
        >`,
    )}
    <span class="spacer"></span>
    ${error && html`<span class="err">${error}</span>`}
    ${task.next &&
    task.status === "active" &&
    html`<button class="primary" disabled=${busy} onClick=${start}>${task.phase ? `Next: ${task.next} → Start` : `Start ${task.next}`}</button>`}
    <label class="check" title="Start each next phase as soon as the previous one is done">
      <input type="checkbox" checked=${task.autoAdvance} disabled=${busy} onChange=${() => toggle("autoAdvance")} /> Auto-advance
    </label>
    <label class="check" title="Agents answer their own questions with their recommendation">
      <input type="checkbox" checked=${task.autonomous} disabled=${busy} onChange=${() => toggle("autonomous")} /> Autonomous
    </label>
  </div>`;
}
