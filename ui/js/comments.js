import { html, useState, useRef, useLayoutEffect } from "/vendor/preact-htm.js";
import { post, del } from "./api.js";
import { cls } from "./util.js";
import { Icon } from "./icons.js";

/** Existing comments on one line, plus an editor when `editing`. */
export function LineComments({ comments, editing, onDone, base, run, inSrc }) {
  const [body, setBody] = useState("");
  const save = async () => {
    if (!body.trim()) return;
    const ok = await run(post("/api/comments", { ...base, body }));
    if (ok) {
      commentsChanged();
      setBody("");
      onDone();
    }
  };
  return html`${comments.map((c) => html`<div key=${c.id} contenteditable="false" class=${cls("cmt", c.status === "sent" && "sent", inSrc && "in-src")}>
      <div class="who"><span class="label">${c.status === "sent" ? "sent to agent" : "you"}</span><span style="flex:1"></span>
        ${c.status === "open" && html`<button class="key small ghost" onClick=${async () => { await run(del(`/api/comments/${c.id}`)); commentsChanged(); }}>delete</button>`}</div>
      <div class="selectable" style="white-space:pre-wrap">${c.body}</div>
    </div>`)}
    ${editing && html`<div contenteditable="false" class=${cls("cmt", inSrc && "in-src")}>
      <${CommentBox} body=${body} setBody=${setBody} onSave=${save} onCancel=${onDone} />
    </div>`}`;
}

/** The text box for a new comment; it takes focus when it appears. */
function CommentBox({ body, setBody, onSave, onCancel }) {
  const ref = useRef(null);
  useLayoutEffect(() => ref.current.focus(), []);
  return html`<textarea ref=${ref} class="field" rows="3" placeholder="Comment for the agent… (⌘↵ to add)" value=${body}
      onInput=${(e) => setBody(e.target.value)}
      onKeyDown=${(e) => {
        if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) onSave();
        if (e.key === "Escape") onCancel();
      }}></textarea>
    <div class="row"><button class="key small" onClick=${onCancel}>Cancel</button><button class="key small primary" onClick=${onSave}>Add comment</button></div>`;
}

/** Tells comment lists to reload after a comment is added or removed. */
export const commentsChanged = () => window.dispatchEvent(new Event("layer8:comments"));

const editable = (el) => el instanceof HTMLElement && /^(INPUT|TEXTAREA)$/.test(el.tagName);

/**
 * Read-only text that can be selected with the mouse, or with the keyboard (click to
 * place the caret, then shift+arrows), to comment on the selection. `locate(range)`
 * returns { path, line } for a selection inside it.
 */
export function Commentable({ base, locate, run, class: klass, children, html: inner }) {
  const ref = useRef(null);
  const [sel, setSel] = useState(null);
  const [writing, setWriting] = useState(false);
  const [body, setBody] = useState("");

  const update = () => {
    if (writing) return;
    const s = getSelection();
    const quote = s.rangeCount ? s.toString().trim() : "";
    if (!quote || !ref.current.contains(s.anchorNode)) return setSel(null);
    const range = s.getRangeAt(0);
    const at = locate(range, quote);
    if (!at) return setSel(null);
    const r = range.getBoundingClientRect();
    setSel({ ...at, quote, x: r.left, y: r.bottom });
  };
  const close = () => {
    setSel(null);
    setWriting(false);
    setBody("");
  };
  const save = async () => {
    if (!body.trim()) return;
    if (await run(post("/api/comments", { ...base, path: sel.path, line: sel.line, quote: sel.quote, body }))) {
      commentsChanged();
      getSelection().removeAllRanges();
      close();
    }
  };

  return html`<div ref=${ref} class=${cls("commentable", klass)} contenteditable="true" spellcheck="false"
      dangerouslySetInnerHTML=${inner === undefined ? undefined : { __html: inner }}
      onBeforeInput=${(e) => !editable(e.target) && e.preventDefault()}
      onMouseUp=${() => setTimeout(update)}
      onKeyUp=${(e) => !editable(e.target) && update()}
      onKeyDown=${(e) => {
        if (editable(e.target)) return;
        if (sel && (e.key === "c" || e.key === "C") && !e.metaKey && !e.ctrlKey) {
          e.preventDefault();
          setWriting(true);
        } else if (e.key === "Escape") {
          e.currentTarget.blur();
          close();
        }
      }}>${inner === undefined ? children : null}</div>
    ${sel && html`<div class="sel-pop" style=${`left:${Math.min(sel.x, innerWidth - 340)}px;top:${Math.min(sel.y + 6, innerHeight - 170)}px`}>
      ${writing
        ? html`<div class="cmt">
            <div class="quote ellipsis">${sel.quote}</div>
            <${CommentBox} body=${body} setBody=${setBody} onSave=${save} onCancel=${close} />
          </div>`
        : html`<button class="key small primary" onMouseDown=${(e) => { e.preventDefault(); setWriting(true); }}><${Icon} name="message-square" /> Comment <kbd>C</kbd></button>`}
    </div>`}`;
}
