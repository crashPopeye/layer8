import { html } from "/vendor/preact-htm.js";
import { Artifacts } from "./artifacts.js";

/** RightPanel holds a task's side tabs; onWide toggles between a narrow and a wide panel. */
export function RightPanel({ taskId, wide, onWide }) {
  return html`<section class="panel">
    <nav class="tabs">
      <a class="sel">Artifacts</a>
      <span class="spacer"></span>
      <button class="icon" title=${wide ? "Narrow panel" : "Widen panel"} onClick=${onWide}>${wide ? "⇥" : "⇤"}</button>
    </nav>
    <${Artifacts} key=${taskId} taskId=${taskId} />
  </section>`;
}
