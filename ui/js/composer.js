import { html, useState, useEffect, useRef } from "/vendor/preact-htm.js";
import { get } from "./api.js";

export const modes = [
  { value: "default", label: "Ask first" },
  { value: "acceptEdits", label: "Auto-accept edits" },
  { value: "auto", label: "Auto" },
  { value: "bypassPermissions", label: "Bypass" },
];

/** nextMode is the permission mode after mode in the ⌥Y cycle. */
export function nextMode(mode) {
  const i = Math.max(0, modes.findIndex((m) => m.value === (mode || "default")));
  return modes[(i + 1) % modes.length].value;
}

/** PermissionChip shows a permission mode; clicking it, or ⌥Y, cycles through the four modes. */
export function PermissionChip({ mode, onChange }) {
  const current = modes.find((m) => m.value === (mode || "default")) || modes[0];
  return html`<button
    type="button"
    class=${`chip ${current.value === "bypassPermissions" ? "danger" : ""}`}
    title="Permission mode (⌥Y to cycle)"
    onClick=${() => onChange(nextMode(current.value))}
  >
    ${current.label}
  </button>`;
}

const models = [
  ["", "Default model"],
  ["opus", "Opus"],
  ["sonnet", "Sonnet"],
  ["haiku", "Haiku"],
];

const efforts = [
  ["", "Default effort"],
  ["low", "Low"],
  ["medium", "Medium"],
  ["high", "High"],
  ["xhigh", "Extra high"],
  ["max", "Max"],
];

/** ModelMenu picks the model and the effort; onChange receives the one field that changed. */
export function ModelMenu({ model, effort, effortTitle, onChange }) {
  const pick = (field, options, value, title) => html`<select class="pick" title=${title} value=${value || ""} onChange=${(e) => onChange({ [field]: e.target.value })}>
    ${options.map(([v, label]) => html`<option value=${v}>${label}</option>`)}
  </select>`;
  return html`${pick("model", models, model, "Model")}${pick("effort", efforts, effort, effortTitle || "Effort")}`;
}

/** tokenAt finds the "/command" (at the start of the message) or "@path" being typed before the caret. */
function tokenAt(text, caret) {
  const m = text.slice(0, caret).match(/(^|\s)([/@])(\S*)$/);
  if (!m) return null;
  const start = caret - m[2].length - m[3].length;
  if (m[2] === "/" && start !== 0) return null;
  return { kind: m[2], start, query: m[3] };
}

/** useCompletions lists what the token being typed can complete to: the task's commands and skills for "/", its files for "@". */
function useCompletions(taskId, token) {
  const [items, setItems] = useState([]);
  const kind = token && token.kind;
  const query = token ? token.query : "";
  useEffect(() => {
    if (!kind || !taskId) return setItems([]);
    let live = true;
    const q = query.toLowerCase();
    const load =
      kind === "@"
        ? get(`/api/tasks/${taskId}/files?q=${encodeURIComponent(query)}`)
        : get(`/api/tasks/${taskId}/commands`).then((all) =>
            all.filter((c) => c.toLowerCase().includes(q)).sort((a, b) => b.toLowerCase().startsWith(q) - a.toLowerCase().startsWith(q)),
          );
    load.then((found) => live && setItems(found.slice(0, 12)), () => live && setItems([]));
    return () => (live = false);
  }, [taskId, kind, query]);
  return items;
}

/** Composer is the message box: ⌘↩ sends, "/" completes commands and skills, "@" completes files,
 * and a queued badge counts messages waiting for the turn to end. */
export function Composer({ taskId, placeholder, queued, busy, onSend, onInterrupt, controls, autofocus }) {
  const [text, setText] = useState("");
  const [caret, setCaret] = useState(0);
  const [index, setIndex] = useState(0);
  const [closed, setClosed] = useState(false);
  const [sending, setSending] = useState(false);
  const [error, setError] = useState("");
  const area = useRef(null);
  const token = closed ? null : tokenAt(text, caret);
  const items = useCompletions(taskId, token);
  const open = token && items.length > 0;
  const at = Math.min(index, items.length - 1);

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
  const accept = (item) => {
    const inserted = `${token.kind}${item} `;
    const next = text.slice(0, token.start) + inserted + text.slice(caret);
    const pos = token.start + inserted.length;
    setText(next);
    setCaret(pos);
    requestAnimationFrame(() => area.current && area.current.setSelectionRange(pos, pos));
  };
  const track = (e) => (setCaret(e.target.selectionStart), setClosed(false));
  const onKeyDown = (e) => {
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) return send(e);
    if (!open) return;
    const moves = { ArrowDown: 1, ArrowUp: -1 };
    if (e.key in moves) {
      e.preventDefault();
      setIndex((at + moves[e.key] + items.length) % items.length);
    } else if (e.key === "Enter" || e.key === "Tab") {
      e.preventDefault();
      accept(items[at]);
    } else if (e.key === "Escape") {
      e.preventDefault();
      setClosed(true);
    }
  };
  return html`<form class="composer" onSubmit=${send}>
    ${open &&
    html`<div class="complete">
      ${items.map(
        (item, i) => html`<div key=${item} class=${`item mono ${i === at ? "sel" : ""}`} onMouseDown=${(e) => (e.preventDefault(), accept(item))}>
          ${token.kind}${item}
        </div>`,
      )}
    </div>`}
    <textarea
      ref=${area}
      autofocus=${autofocus}
      placeholder=${placeholder || "Type your message…  / commands · @ files"}
      value=${text}
      onInput=${(e) => (setText(e.target.value), setIndex(0), track(e))}
      onKeyUp=${(e) => !["ArrowDown", "ArrowUp", "Enter", "Tab", "Escape"].includes(e.key) && setCaret(e.target.selectionStart)}
      onClick=${track}
      onKeyDown=${onKeyDown}
    ></textarea>
    <div class="bar">
      ${controls}
      ${queued > 0 && html`<span class="tag warn">${queued} queued</span>`}
      ${error && html`<span class="err">${error}</span>`}
      <span class="spacer"></span>
      ${busy && onInterrupt && html`<button type="button" onClick=${onInterrupt}>Interrupt</button>`}
      <button class="primary" type="submit" disabled=${sending || !text.trim()}>${busy ? "Queue ⌘↩" : "Send ⌘↩"}</button>
    </div>
  </form>`;
}
