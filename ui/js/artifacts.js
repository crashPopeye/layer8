import { html, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post } from "./api.js";
import { useEvents, useSessions } from "./events.js";

const SANDBOX = "allow-scripts allow-popups allow-popups-to-escape-sandbox";

/** kind says how an artifact is shown: rendered Markdown, a sandboxed page, an image or plain text. */
function kind(path) {
  const ext = path.toLowerCase().split(".").pop();
  if (ext === "md" || ext === "markdown") return "md";
  if (ext === "html" || ext === "htm") return "html";
  if (["svg", "png", "jpg", "jpeg", "gif", "webp"].includes(ext)) return "image";
  return "text";
}

function ago(ms) {
  const s = Math.max(0, (Date.now() - ms) / 1000);
  if (s < 60) return "now";
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

/** useTaskList fetches path and refetches it on the daemon's kind events for the task. */
function useTaskList(taskId, path, kind) {
  const [items, setItems] = useState([]);
  const load = useCallback(() => get(path).then(setItems, () => {}), [path]);
  useEffect(() => void load(), [load]);
  useEvents((e) => (e.kind === "open" || (e.kind === kind && e.id === taskId)) && load());
  return items;
}

let mermaidReady = null;

/** loadMermaid loads the vendored Mermaid once and themes it from the palette's CSS variables. */
function loadMermaid() {
  mermaidReady ||= new Promise((resolve, reject) => {
    const script = document.createElement("script");
    script.src = "/vendor/mermaid.min.js";
    script.onload = () => resolve(window.mermaid);
    script.onerror = () => reject(new Error("could not load Mermaid"));
    document.head.append(script);
  }).then((mermaid) => {
    const css = getComputedStyle(document.documentElement);
    const v = (name) => css.getPropertyValue(name).trim();
    mermaid.initialize({
      startOnLoad: false,
      securityLevel: "strict",
      suppressErrorRendering: true,
      htmlLabels: false,
      flowchart: { htmlLabels: false },
      theme: "base",
      fontFamily: css.getPropertyValue("--sans"),
      themeVariables: {
        fontSize: "14px",
        background: v("--terminal-bg"),
        primaryColor: v("--terminal-bg-alt"),
        primaryBorderColor: v("--terminal-accent"),
        primaryTextColor: v("--terminal-fg"),
        secondaryColor: v("--terminal-bg-alt"),
        secondaryBorderColor: v("--terminal-accent-alt"),
        tertiaryColor: v("--terminal-bg"),
        tertiaryBorderColor: v("--terminal-border"),
        lineColor: v("--terminal-fg-dim"),
        textColor: v("--terminal-fg"),
        titleColor: v("--terminal-fg"),
        edgeLabelBackground: v("--terminal-bg"),
        clusterBkg: v("--terminal-bg"),
        clusterBorder: v("--terminal-border"),
        noteBkgColor: v("--terminal-bg-alt"),
        noteBorderColor: v("--terminal-accent-alt"),
        noteTextColor: v("--terminal-fg"),
        actorBkg: v("--terminal-bg-alt"),
        actorBorder: v("--terminal-accent"),
        actorTextColor: v("--terminal-fg"),
        signalColor: v("--terminal-fg-dim"),
        signalTextColor: v("--terminal-fg"),
      },
    });
    return mermaid;
  });
  return mermaidReady;
}

let diagrams = 0;

/** renderMermaid swaps every <pre class="mermaid"> under root for its diagram, with a "Show code" toggle. */
async function renderMermaid(root) {
  const blocks = [...root.querySelectorAll("pre.mermaid")];
  if (!blocks.length) return;
  const mermaid = await loadMermaid();
  for (const pre of blocks) {
    const code = pre.textContent;
    try {
      const { svg } = await mermaid.render(`mermaid-${++diagrams}`, code);
      const figure = document.createElement("figure");
      figure.className = "diagram";
      figure.dataset.line = pre.dataset.line;
      figure.innerHTML = svg;
      const details = document.createElement("details");
      details.innerHTML = "<summary>Show code</summary>";
      const source = document.createElement("pre");
      source.textContent = code;
      details.append(source);
      figure.append(details);
      pre.replaceWith(figure);
    } catch (err) {
      pre.classList.add("broken");
      pre.title = err.message;
    }
  }
}

/** selectionAt returns the line and text the user selected inside root, if any. */
function selectionAt(root) {
  const sel = window.getSelection();
  if (!sel || sel.isCollapsed || !root.contains(sel.anchorNode)) return null;
  const quote = sel.toString().trim();
  if (!quote) return null;
  const node = sel.anchorNode.nodeType === Node.ELEMENT_NODE ? sel.anchorNode : sel.anchorNode.parentElement;
  const block = node.closest("[data-line]");
  const rect = sel.getRangeAt(0).getBoundingClientRect();
  const box = root.getBoundingClientRect();
  return {
    line: block ? Number(block.dataset.line) : 0,
    quote: quote.slice(0, 500),
    top: rect.bottom - box.top + root.scrollTop + 4,
    left: Math.max(0, rect.left - box.left),
  };
}

/** Markdown shows server-rendered HTML, then draws its diagrams and wires its links. */
function Markdown({ taskId, artifact, comments, onOpen }) {
  const [doc, setDoc] = useState(null);
  const ref = useRef(null);
  useEffect(() => {
    get(`/api/tasks/${taskId}/render/${encodeURI(artifact.path)}`).then(
      (r) => setDoc({ html: r.html }),
      (err) => setDoc({ error: err.message }),
    );
  }, [taskId, artifact.path, artifact.modTime]);
  useEffect(() => {
    const root = ref.current;
    if (!root || !doc || doc.error) return;
    renderMermaid(root);
    for (const a of root.querySelectorAll("a[href]")) {
      if (/^[a-z]+:/i.test(a.getAttribute("href"))) (a.target = "_blank"), (a.rel = "noopener");
    }
  }, [doc]);
  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    root.querySelectorAll(".commented").forEach((el) => el.classList.remove("commented"));
    for (const c of comments) {
      if (c.status === "open" && c.line) root.querySelector(`[data-line="${c.line}"]`)?.classList.add("commented");
    }
  }, [doc, comments]);
  const onClick = (e) => {
    const a = e.target.closest("a[href]");
    if (!a) return;
    const href = a.getAttribute("href");
    const target = a.dataset.artifact || (!/^([a-z]+:|\/|#)/i.test(href) && decodeURI(href.split("#")[0]));
    if (target) {
      e.preventDefault();
      onOpen(target);
    }
  };
  if (!doc) return null;
  if (doc.error) return html`<div class="err">${doc.error}</div>`;
  return html`<div class="md" ref=${ref} onClick=${onClick} dangerouslySetInnerHTML=${{ __html: doc.html }}></div>`;
}

/** Text shows a plain file with numbered lines. */
function Text({ artifact }) {
  const [lines, setLines] = useState(null);
  useEffect(() => {
    fetch(artifact.url)
      .then((r) => r.text())
      .then((t) => setLines(t.split("\n")), (err) => setLines([err.message]));
  }, [artifact.url, artifact.modTime]);
  if (!lines) return null;
  return html`<div class="text mono">
    ${lines.map((l, i) => html`<div data-line=${i + 1}><span class="ln">${i + 1}</span>${l}</div>`)}
  </div>`;
}

/** CommentForm writes a comment on a line, or on the whole file when line is 0. */
function CommentForm({ taskId, path, at, onDone }) {
  const [body, setBody] = useState("");
  const [error, setError] = useState("");
  const save = async (e) => {
    e && e.preventDefault();
    if (!body.trim()) return;
    try {
      await post(`/api/tasks/${taskId}/comments`, { target: "artifact", path, line: at.line, quote: at.quote, body });
      onDone();
    } catch (err) {
      setError(err.message);
    }
  };
  return html`<form class="comment-form" onSubmit=${save}>
    <div class="lbl">${at.line ? `Comment on line ${at.line}` : "Comment on this file"}</div>
    ${at.quote && html`<blockquote>${at.quote}</blockquote>`}
    <textarea
      autofocus
      placeholder="What should change? ⌘↩ saves"
      value=${body}
      onInput=${(e) => setBody(e.target.value)}
      onKeyDown=${(e) => (e.key === "Enter" && (e.metaKey || e.ctrlKey) && save(e)) || (e.key === "Escape" && onDone())}
    ></textarea>
    <div class="btns">
      ${error && html`<span class="err">${error}</span>`}
      <button type="button" onClick=${onDone}>Cancel</button>
      <button class="primary" type="submit" disabled=${!body.trim()}>Comment</button>
    </div>
  </form>`;
}

/** Doc is the viewer of one artifact; selecting text in it offers a line comment. */
function Doc({ taskId, artifact, comments, onOpen }) {
  const [pick, setPick] = useState(null);
  const [writing, setWriting] = useState(null);
  const scroller = useRef(null);
  const k = kind(artifact.path);
  const onMouseUp = () => setTimeout(() => setPick(scroller.current && selectionAt(scroller.current)), 0);
  return html`<div class="viewer">
    <div class="viewer-head">
      <span class="mono path" title=${artifact.path}>${artifact.path}</span>
      <span class="lbl">${artifact.type}</span>
      <span class="spacer"></span>
      <button onClick=${() => setWriting({ line: 0, quote: "" })}>Comment</button>
      <a class="btn" href=${artifact.url} target="_blank" rel="noopener" title="Open on its own">↗</a>
    </div>
    <div class="viewer-body" ref=${scroller} onMouseUp=${onMouseUp}>
      ${k === "md" && html`<${Markdown} taskId=${taskId} artifact=${artifact} comments=${comments} onOpen=${onOpen} />`}
      ${k === "html" && html`<iframe class="artifact page" sandbox=${SANDBOX} src=${artifact.url} title=${artifact.path}></iframe>`}
      ${k === "image" && html`<img class="artifact" src=${artifact.url} alt=${artifact.path} />`}
      ${k === "text" && html`<${Text} artifact=${artifact} />`}
      ${pick &&
      !writing &&
      html`<button class="primary float" style=${{ top: `${pick.top}px`, left: `${pick.left}px` }} onMouseUp=${(e) => e.stopPropagation()} onClick=${() => (setWriting(pick), setPick(null))}>
        Comment
      </button>`}
    </div>
    ${writing && html`<${CommentForm} taskId=${taskId} path=${artifact.path} at=${writing} onDone=${() => setWriting(null)} />`}
  </div>`;
}

/** Comments lists a task's open artifact comments and sends them to a session of the task. */
function Comments({ taskId, comments, onOpen }) {
  const sessions = useSessions(taskId);
  const [sessionId, setSessionId] = useState("");
  const [error, setError] = useState("");
  const open = comments.filter((c) => c.status === "open" && c.target === "artifact");
  const target = sessionId || (sessions.length ? sessions[sessions.length - 1].id : "");
  if (!open.length) return null;
  const send = (body) =>
    post(`/api/tasks/${taskId}/comments/send`, { sessionId: target, ...body }).then(
      () => setError(""),
      (err) => setError(err.message),
    );
  return html`<div class="comments">
    <div class="comments-head">
      <span class="lbl">${open.length} open comment${open.length > 1 ? "s" : ""}</span>
      <span class="spacer"></span>
      <select value=${target} onChange=${(e) => setSessionId(e.target.value)} title="Session that receives the comments">
        ${!sessions.length && html`<option value="">no session</option>`}
        ${sessions.map((s) => html`<option value=${s.id}>${s.label}</option>`)}
      </select>
      <button class="primary" disabled=${!target} onClick=${() => send({ resolve: true })}>Send all & resolve</button>
    </div>
    ${error && html`<div class="err">${error}</div>`}
    ${open.map(
      (c) => html`<div class="comment" key=${c.id}>
        <div class="where mono" onClick=${() => onOpen(c.path)}>${c.path}${c.line ? `:${c.line}` : ""}</div>
        ${c.quote && html`<blockquote>${c.quote}</blockquote>`}
        <div class="note">${c.body}</div>
        <div class="btns"><button disabled=${!target} onClick=${() => send({ ids: [c.id] })}>Send to agent</button></div>
      </div>`,
    )}
  </div>`;
}

/** Artifacts is the panel tab listing a task's artifacts, newest first, with a viewer for the chosen one. */
export function Artifacts({ taskId }) {
  const artifacts = useTaskList(taskId, `/api/tasks/${taskId}/artifacts`, "artifacts");
  const comments = useTaskList(taskId, `/api/tasks/${taskId}/comments`, "comments");
  const [selected, setSelected] = useState("");
  const current = artifacts.find((a) => a.path === selected) || artifacts[0];
  const counts = {};
  for (const c of comments) if (c.status === "open") counts[c.path] = (counts[c.path] || 0) + 1;
  return html`<div class="artifacts">
    <div class="files">
      ${artifacts.map(
        (a) => html`<div key=${a.path} class=${`file ${a === current ? "sel" : ""}`} onClick=${() => setSelected(a.path)}>
          <span class="name" title=${a.path}>${a.path}</span>
          ${counts[a.path] && html`<span class="tag warn">${counts[a.path]}</span>`}
          <span class="lbl">${a.type}</span>
          <span class="dim when">${ago(a.modTime)}</span>
        </div>`,
      )}
      ${!artifacts.length && html`<div class="dim">No artifacts yet. Agents write them to .layer8/task/.</div>`}
    </div>
    ${current &&
    html`<${Doc}
      key=${current.path}
      taskId=${taskId}
      artifact=${current}
      comments=${comments.filter((c) => c.path === current.path)}
      onOpen=${setSelected}
    />`}
    <${Comments} taskId=${taskId} comments=${comments} onOpen=${setSelected} />
  </div>`;
}
