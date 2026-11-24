import { html, useState } from "/vendor/preact-htm.js";

export const modes = [
  { value: "default", label: "Ask first" },
  { value: "acceptEdits", label: "Auto-accept edits" },
  { value: "auto", label: "Auto" },
  { value: "bypassPermissions", label: "Bypass" },
];

/** PermissionChip shows a permission mode; when onChange is set, clicking cycles through the four modes. */
export function PermissionChip({ mode, onChange }) {
  const i = Math.max(0, modes.findIndex((m) => m.value === (mode || "default")));
  const next = () => onChange && onChange(modes[(i + 1) % modes.length].value);
  return html`<button
    type="button"
    class=${`chip ${modes[i].value === "bypassPermissions" ? "danger" : ""}`}
    title=${onChange ? "Permission mode (click to change)" : "Permission mode of this session"}
    disabled=${!onChange}
    onClick=${next}
  >
    ${modes[i].label}
  </button>`;
}

/** Composer is the message box: ⌘↩ sends, a queued badge counts messages waiting for the turn to end. */
export function Composer({ placeholder, queued, busy, onSend, onInterrupt, chip, autofocus }) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const send = async (e) => {
    e && e.preventDefault();
    if (!text.trim() || sending) return;
    setSending(true);
    setError("");
    try {
      await onSend(text);
      setText("");
    } catch (err) {
      setError(err.message);
    }
    setSending(false);
  };
  const onKeyDown = (e) => e.key === "Enter" && (e.metaKey || e.ctrlKey) && send(e);
  return html`<form class="composer" onSubmit=${send}>
    <textarea
      autofocus=${autofocus}
      placeholder=${placeholder || "Type your message…"}
      value=${text}
      onInput=${(e) => setText(e.target.value)}
      onKeyDown=${onKeyDown}
    ></textarea>
    <div class="bar">
      ${chip}
      ${queued > 0 && html`<span class="tag warn">${queued} queued</span>`}
      ${error && html`<span class="err">${error}</span>`}
      <span class="spacer"></span>
      ${busy && onInterrupt && html`<button type="button" onClick=${onInterrupt}>Interrupt</button>`}
      <button class="primary" type="submit" disabled=${sending || !text.trim()}>${busy ? "Queue ⌘↩" : "Send ⌘↩"}</button>
    </div>
  </form>`;
}
