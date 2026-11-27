// Draws a layout tree from layout.js: splits with draggable dividers, tab groups
// whose tabs drag onto another group's centre (join) or edge (split beside it).
import { html, useState, useRef } from "/vendor/preact-htm.js";
import { cls } from "./util.js";
import { Icon } from "./icons.js";
import { ops } from "./layout.js";

let dragging = null; // the tab being dragged: { tabId }

/** Renders the layout tree; panels(id) gives a panel's definition, render(def) its body. */
export function Dock({ layout, onChange, panels, render }) {
  const node = (n) => (n.tabs ? html`<${Group} key=${n.id} g=${n} layout=${layout} onChange=${onChange} panels=${panels} render=${render} />` : html`<${Split} key=${n.id} s=${n} layout=${layout} onChange=${onChange} node=${node} />`);
  return html`<div class="dock">${node(layout)}</div>`;
}

function Split({ s, layout, onChange, node }) {
  const box = useRef(null);
  const [live, setLive] = useState(null);
  const sizes = live || s.sizes;
  const drag = (i) => (e) => {
    e.preventDefault();
    const rect = box.current.getBoundingClientRect();
    const total = s.dir === "row" ? rect.width : rect.height;
    const start = s.dir === "row" ? e.clientX : e.clientY;
    let next = s.sizes;
    const move = (ev) => {
      const d = ((s.dir === "row" ? ev.clientX : ev.clientY) - start) / total;
      const pair = s.sizes[i] + s.sizes[i + 1];
      const a = Math.min(pair - 0.06, Math.max(0.06, s.sizes[i] + d));
      next = s.sizes.map((x, j) => (j === i ? a : j === i + 1 ? pair - a : x));
      setLive(next);
    };
    const up = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", up);
      setLive(null);
      onChange(ops.resize(layout, s.id, next));
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", up);
  };
  return html`<div ref=${box} class=${cls("split", s.dir)}>
    ${s.children.map((c, i) => [
      i > 0 && html`<div key=${"d" + c.id} class="divider" onPointerDown=${drag(i - 1)}></div>`,
      html`<div key=${c.id} class="cell" style=${{ flexGrow: sizes[i] }}>${node(c)}</div>`,
    ])}
  </div>`;
}

const zoneOf = (e, el) => {
  const r = el.getBoundingClientRect();
  const x = (e.clientX - r.left) / r.width;
  const y = (e.clientY - r.top) / r.height;
  const edge = Math.min(x, 1 - x, y, 1 - y);
  if (edge > 0.25) return "center";
  return edge === x ? "left" : edge === 1 - x ? "right" : edge === y ? "top" : "bottom";
};

function Group({ g, layout, onChange, panels, render }) {
  const [zone, setZone] = useState(null);
  const [menu, setMenu] = useState(false);
  const active = g.tabs.find((t) => t.id === g.active) || g.tabs[0];
  const def = panels(active.panel);
  const over = (e) => {
    if (!dragging) return;
    e.preventDefault();
    setZone(zoneOf(e, e.currentTarget));
  };
  const drop = (e) => {
    e.preventDefault();
    const z = zone;
    setZone(null);
    if (dragging) onChange(ops.move(layout, dragging.tabId, g.id, z || "center"));
    dragging = null;
  };
  return html`<section class="group" onDragOver=${over} onDragLeave=${(e) => !e.currentTarget.contains(e.relatedTarget) && setZone(null)} onDrop=${drop}>
    <header class="group-head">
      <div class="group-tabs">
        ${g.tabs.map((t) => {
          const d = panels(t.panel);
          return html`<div key=${t.id} class="gtab" draggable="true" aria-selected=${t.id === active.id} title=${d.title}
            onDragStart=${(e) => { dragging = { tabId: t.id }; e.dataTransfer.effectAllowed = "move"; e.dataTransfer.setData("text/plain", t.panel); }}
            onDragEnd=${() => { dragging = null; setZone(null); }}
            onClick=${() => onChange(ops.activate(layout, g.id, t.id))}>
            <${Icon} name=${d.icon || "panels-top-left"} size=${13} /><span class="ellipsis">${d.title}</span>${d.badge ? html`<span class="n">${d.badge}</span>` : ""}
            <button class="x" title="Close panel" onClick=${(e) => { e.stopPropagation(); onChange(ops.close(layout, t.id)); }}><${Icon} name="x" size=${11} /></button>
          </div>`;
        })}
      </div>
      <div class="group-tools">
        <button class="gbtn" title="Add a panel here" onClick=${() => setMenu(!menu)}><${Icon} name="plus" size=${14} /></button>
        <button class="gbtn" title="Split right" onClick=${() => onChange(ops.split(layout, g.id, "right", active.panel))}><${Icon} name="columns-2" size=${14} /></button>
        <button class="gbtn" title="Split down" onClick=${() => onChange(ops.split(layout, g.id, "bottom", active.panel))}><${Icon} name="rows-2" size=${14} /></button>
      </div>
      ${menu && html`<div class="menu group-menu" onMouseLeave=${() => setMenu(false)}>
        ${panels().map((d) => html`<button key=${d.id} class="item" onClick=${() => { setMenu(false); onChange(ops.add(layout, g.id, d.id)); }}>
          <${Icon} name=${d.icon || "panels-top-left"} /> ${d.title}
        </button>`)}
      </div>`}
    </header>
    <div class="group-body">${render(def)}</div>
    ${zone && html`<div class=${cls("dropzone", zone)}></div>`}
  </section>`;
}
