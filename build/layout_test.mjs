// Checks the window's layout tree operations: node build/layout_test.mjs
import assert from "node:assert/strict";
import { ops, defaultLayout } from "../ui/js/layout.js";

const groups = (n) => (n.tabs ? [n] : n.children.flatMap(groups));
const shape = (n) => (n.tabs ? n.tabs.map((t) => t.panel).join("+") : `${n.dir}(${n.children.map(shape).join(", ")})`);

let l = defaultLayout();
assert.equal(shape(l), "row(tasks, sessions+changes+pr+docs, task)");

const docs = groups(l)[1].tabs.find((t) => t.panel === "docs");
l = ops.move(l, docs.id, groups(l)[2].id, "bottom");
assert.equal(shape(l), "row(tasks, sessions+changes+pr, col(task, docs))");

l = ops.move(l, groups(l)[0].tabs[0].id, groups(l)[1].id, "center");
assert.equal(shape(l), "row(sessions+changes+pr+tasks, col(task, docs))", "an emptied group disappears");
assert.ok(Math.abs(l.sizes.reduce((a, b) => a + b) - 1) < 1e-9, "sizes still sum to 1");

l = ops.split(l, groups(l)[0].id, "right", "sessions");
assert.equal(shape(l), "row(sessions+changes+pr+tasks, sessions, col(task, docs))", "a split in the same direction joins the row");

for (const t of groups(l)[0].tabs.slice()) l = ops.close(l, t.id);
assert.equal(shape(l), "row(sessions, col(task, docs))");

l = ops.reveal(l, "pr");
assert.ok(groups(l).some((g) => g.tabs.find((t) => t.id === g.active).panel === "pr"), "reveal adds and activates a missing panel");

for (const g of groups(l)) for (const t of g.tabs.slice()) l = ops.close(l, t.id);
assert.equal(shape(l), shape(defaultLayout()), "closing everything falls back to the default layout");

assert.deepEqual(JSON.parse(JSON.stringify(l)), l, "a layout is plain JSON");
console.log("layout ok");
