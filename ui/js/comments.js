import { html, useState } from "/vendor/preact-htm.js";
import { post, del } from "./api.js";
import { cls } from "./util.js";

/** Existing comments on one line, plus an editor when `editing`. */
export function LineComments({ comments, editing, onDone, base, run, inSrc }) {
  const [body, setBody] = useState("");
  const save = async () => {
    if (!body.trim()) return;
    const ok = await run(post("/api/comments", { ...base, body }));
    if (ok) {
      setBody("");
      onDone();
    }
  };
  return html`${comments.map((c) => html`<div key=${c.id} class=${cls("cmt", c.status === "sent" && "sent", inSrc && "in-src")}>
      <div class="who"><span class="label">${c.status === "sent" ? "sent to agent" : "you"}</span><span style="flex:1"></span>
        ${c.status === "open" && html`<button class="key small ghost" onClick=${() => run(del(`/api/comments/${c.id}`))}>delete</button>`}</div>
      <div class="selectable" style="white-space:pre-wrap">${c.body}</div>
    </div>`)}
    ${editing && html`<div class=${cls("cmt", inSrc && "in-src")}>
      <textarea class="field" rows="3" ref=${(el) => el && !el.dataset.f && (el.dataset.f = "1", setTimeout(() => el.focus(), 20))} placeholder="Comment for the agent…" value=${body}
        onInput=${(e) => setBody(e.target.value)}
        onKeyDown=${(e) => {
          if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) save();
          if (e.key === "Escape") onDone();
        }}></textarea>
      <div class="row"><button class="key small" onClick=${onDone}>Cancel</button><button class="key small primary" onClick=${save}>Add comment</button></div>
    </div>`}`;
}
