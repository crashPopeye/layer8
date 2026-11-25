import { html } from "/vendor/preact-htm.js";
import { Artifacts } from "./artifacts.js";
import { Changes, useChanges } from "./changes.js";
import { PR } from "./pr.js";

export const tabs = ["artifacts", "changes", "pr"];

/** RightPanel holds a task's Artifacts, Changes and PR tabs, tab being the one shown; onWide toggles
 * between a narrow and a wide panel. */
export function RightPanel({ taskId, tab, onTab, wide, onWide }) {
  const changes = useChanges(taskId);
  const count = changes.data ? changes.data.files.length : 0;
  const tabLink = (t, label) => html`<a class=${tab === t ? "sel" : ""} title=${`⌘${tabs.indexOf(t) + 1}`} onClick=${() => onTab(t)}>${label}</a>`;
  return html`<section class="panel">
    <nav class="tabs">
      ${tabLink("artifacts", "Artifacts")} ${tabLink("changes", count ? `Changes · ${count}` : "Changes")} ${tabLink("pr", "PR")}
      <span class="spacer"></span>
      <button class="icon" title=${wide ? "Narrow panel" : "Widen panel"} onClick=${onWide}>${wide ? "⇥" : "⇤"}</button>
    </nav>
    ${tab === "artifacts" && html`<${Artifacts} key=${taskId} taskId=${taskId} />`}
    ${tab === "changes" && html`<${Changes} key=${taskId} taskId=${taskId} changes=${changes} />`}
    ${tab === "pr" && html`<${PR} key=${taskId} taskId=${taskId} />`}
  </section>`;
}
