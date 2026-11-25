import { html, useState } from "/vendor/preact-htm.js";
import { StatusDot } from "./conversation.js";

/** entries flattens tasks and sessions into what the palette searches. */
function entries(tasks, sessions) {
  const titles = new Map(tasks.map((t) => [t.id, t.title]));
  return [
    ...tasks.map((t) => ({ key: t.id, kind: "task", title: t.title, detail: [t.ticket, t.branch, t.status !== "active" && t.status].filter(Boolean).join(" · "), task: t.id })),
    ...sessions.map((s) => ({ key: s.id, kind: "session", title: s.label, detail: titles.get(s.taskId) || "", task: s.taskId, session: s.id, status: s.status })),
  ];
}

/** Palette is the ⌘K search over tasks and sessions; every word typed must appear in the title or detail. */
export function Palette({ tasks, sessions, onPick, onClose }) {
  const [q, setQ] = useState("");
  const [index, setIndex] = useState(0);
  const words = q.toLowerCase().split(/\s+/).filter(Boolean);
  const hits = entries(tasks, sessions)
    .filter((it) => words.every((w) => `${it.title} ${it.detail}`.toLowerCase().includes(w)))
    .slice(0, 50);
  const at = Math.min(index, Math.max(0, hits.length - 1));
  const pick = (it) => it && (onPick(it.task, it.session), onClose());
  const onKeyDown = (e) => {
    const moves = { ArrowDown: 1, ArrowUp: -1 };
    if (e.key in moves) {
      e.preventDefault();
      setIndex((at + moves[e.key] + hits.length) % Math.max(1, hits.length));
    } else if (e.key === "Enter") {
      e.preventDefault();
      pick(hits[at]);
    } else if (e.key === "Escape") {
      onClose();
    }
  };
  return html`<div class="palette" onClick=${(e) => e.target === e.currentTarget && onClose()}>
    <div class="box">
      <input autofocus placeholder="Search tasks and sessions…" value=${q} onInput=${(e) => (setQ(e.target.value), setIndex(0))} onKeyDown=${onKeyDown} />
      <div class="list">
        ${hits.map(
          (it, i) => html`<div key=${it.key} class=${`item ${i === at ? "sel" : ""}`} onMouseMove=${() => setIndex(i)} onClick=${() => pick(it)}>
            ${it.kind === "session" ? html`<${StatusDot} status=${it.status} />` : html`<span class="lbl">task</span>`}
            <span class="title">${it.title}</span>
            <span class="dim detail">${it.detail}</span>
          </div>`,
        )}
        ${!hits.length && html`<div class="item dim">Nothing matches.</div>`}
      </div>
    </div>
  </div>`;
}
