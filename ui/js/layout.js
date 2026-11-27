// The window is a tree: splits (rows or columns of children with relative sizes)
// whose leaves are tab groups of panels. Every change copies the tree, edits it and
// tidies it, so a layout is always plain JSON that can be saved as is.
let seq = 0;
const uid = (p) => `${p}${Date.now().toString(36)}${(seq++).toString(36)}`;

export const group = (...panels) => {
  const tabs = panels.map((panel) => ({ id: uid("t"), panel }));
  return { id: uid("g"), tabs, active: tabs[0] && tabs[0].id };
};
const split = (dir, children, sizes) => ({ id: uid("s"), dir, children, sizes: sizes || children.map(() => 1 / children.length) });

export const defaultLayout = () =>
  split("row", [group("tasks"), group("sessions", "changes", "pr", "docs"), group("task")], [0.19, 0.57, 0.24]);

const groups = (n) => (n.tabs ? [n] : n.children.flatMap(groups));
const findGroup = (root, id) => groups(root).find((g) => g.id === id);

function parentOf(n, id) {
  if (n.tabs) return null;
  for (const c of n.children) {
    if (c.id === id) return n;
    const p = parentOf(c, id);
    if (p) return p;
  }
  return null;
}

/** Drops empty groups, unwraps one-child splits and merges a split into a parent of the same direction. */
function tidy(n) {
  if (n.tabs) {
    if (!n.tabs.some((t) => t.id === n.active)) n.active = n.tabs[0] && n.tabs[0].id;
    return n.tabs.length ? n : null;
  }
  const kids = [];
  const sizes = [];
  n.children.forEach((c, i) => {
    const t = tidy(c);
    if (!t) return;
    if (!t.tabs && t.dir === n.dir) {
      t.children.forEach((cc, j) => {
        kids.push(cc);
        sizes.push(n.sizes[i] * t.sizes[j]);
      });
    } else {
      kids.push(t);
      sizes.push(n.sizes[i]);
    }
  });
  if (!kids.length) return null;
  if (kids.length === 1) return kids[0];
  const total = sizes.reduce((a, b) => a + b, 0);
  return { ...n, children: kids, sizes: sizes.map((s) => s / total) };
}

const edit = (root, fn) => {
  const copy = structuredClone(root);
  fn(copy);
  return tidy(copy) || defaultLayout();
};

function takeTab(root, tabId) {
  for (const g of groups(root)) {
    const i = g.tabs.findIndex((t) => t.id === tabId);
    if (i >= 0) return g.tabs.splice(i, 1)[0];
  }
  return null;
}

/** Puts node beside the group (or the whole tree) on the given side. */
function place(root, gid, side, node) {
  const dir = side === "left" || side === "right" ? "row" : "col";
  const before = side === "left" || side === "top";
  const parent = parentOf(root, gid);
  if (parent && parent.dir === dir) {
    const i = parent.children.findIndex((c) => c.id === gid);
    const half = parent.sizes[i] / 2;
    parent.sizes.splice(i, 1, half, half);
    parent.children.splice(before ? i : i + 1, 0, node);
    return;
  }
  const target = gid === root.id ? root : findGroup(root, gid);
  const moved = { ...target };
  for (const k of Object.keys(target)) delete target[k];
  Object.assign(target, split(dir, before ? [node, moved] : [moved, node]));
}

export const ops = {
  activate: (root, gid, tabId) => edit(root, (r) => (findGroup(r, gid).active = tabId)),
  close: (root, tabId) => edit(root, (r) => takeTab(r, tabId)),
  add: (root, gid, panel) =>
    edit(root, (r) => {
      const g = findGroup(r, gid);
      const t = { id: uid("t"), panel };
      g.tabs.push(t);
      g.active = t.id;
    }),
  split: (root, gid, side, panel) => edit(root, (r) => place(r, gid, side, group(panel))),
  move: (root, tabId, gid, zone) =>
    edit(root, (r) => {
      const t = takeTab(r, tabId);
      const g = findGroup(r, gid);
      if (!t) return;
      if (zone === "center") {
        g.tabs.push(t);
        g.active = t.id;
        return;
      }
      place(r, gid, zone, { id: uid("g"), tabs: [t], active: t.id });
    }),
  resize: (root, sid, sizes) =>
    edit(root, (r) => {
      const find = (n) => (n.id === sid ? n : n.tabs ? null : n.children.map(find).find(Boolean));
      find(r).sizes = sizes;
    }),
  /** Brings a panel to the front, adding it to the largest group if no group has it. */
  reveal: (root, panel) =>
    edit(root, (r) => {
      const all = groups(r);
      const g = all.find((x) => x.tabs.some((t) => t.panel === panel));
      if (g) g.active = g.tabs.find((t) => t.panel === panel).id;
      else {
        const big = all.reduce((a, b) => (b.tabs.length > a.tabs.length ? b : a));
        const t = { id: uid("t"), panel };
        big.tabs.push(t);
        big.active = t.id;
      }
    }),
};

