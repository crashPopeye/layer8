// Panels are what the dock arranges. Built-in panels and plugin panels have the same
// shape: { id, title, icon, needsTask?, badge?(ctx), Panel(ctx) }, where ctx is the
// window's shared focus and state.
import { html, h, useState, useEffect, useRef, useMemo, useCallback } from "/vendor/preact-htm.js";
import { get, post, put, del, pluginURL } from "./api.js";
import { cls } from "./util.js";
import { Icon } from "./icons.js";
import { TaskPanel } from "./task.js";
import { SessionsPane } from "./sessions.js";
import { ChangesPane } from "./changes.js";
import { PRPane } from "./pr.js";
import { DocsPane } from "./docs.js";

export const act = (ref, verb, args = {}) => post("/api/act", { ref, verb, args });
export const look = (ref = "") => get(`/api/look?ref=${encodeURIComponent(ref)}`);

export const builtins = [
  { id: "tasks", title: "Tasks", icon: "layout-dashboard", Panel: (c) => html`<${Rail} ...${c} />` },
  { id: "sessions", title: "Sessions", icon: "messages-square", needsTask: true, badge: (c) => c.sessions.length || null,
    Panel: (c) => html`<${SessionsPane} state=${c.state} task=${c.task} slice=${c.slice} sessions=${c.sessions} session=${c.session}
      onSelect=${(id) => c.setFocus({ ...c.focus, sessionId: id })} run=${c.run} onStart=${c.start} />` },
  { id: "changes", title: "Changes", icon: "file-diff", needsTask: true,
    Panel: (c) => (c.task.branch ? html`<${ChangesPane} key=${c.focus.sliceId} task=${c.task} slice=${c.slice} state=${c.state} run=${c.run} />` : html`<div class="empty">Not a git repo: there is nothing to diff.</div>`) },
  { id: "pr", title: "Pull request", icon: "git-pull-request", needsTask: true, badge: (c) => (c.slice && c.slice.openThreads) || null,
    Panel: (c) => html`<${PRPane} key=${c.focus.sliceId} task=${c.task} slice=${c.slice} slices=${c.slices} run=${c.run} onSession=${c.showSession} />` },
  { id: "docs", title: "Docs", icon: "file-text", needsTask: true,
    Panel: (c) => html`<${DocsPane} task=${c.task} slice=${c.slice} state=${c.state} run=${c.run} />` },
  { id: "task", title: "Task", icon: "list-checks", needsTask: true, Panel: (c) => html`<${TaskPanel} ...${c} />` },
];

/** What a plugin receives: the same building blocks the built-in panels use. */
const sdk = { html, h, useState, useEffect, useRef, useMemo, useCallback, Icon, cls, api: { get, post, put, del }, act, look };

/** Loads ~/.layer8/plugins/*.js. A broken plugin is reported and skipped. */
export async function loadPlugins(names) {
  const out = { panels: [], commands: [], errors: [] };
  for (const name of names) {
    try {
      const mod = await import(pluginURL(name));
      const p = (typeof mod.default === "function" ? mod.default(sdk) : mod.default) || {};
      out.panels.push(...(p.panels || []).map((d) => ({ icon: "puzzle", ...d })));
      out.commands.push(...(p.commands || []).map((c) => ({ group: "Plugin", ...c })));
    } catch (e) {
      out.errors.push(`${name}: ${e.message}`);
    }
  }
  return out;
}

function Rail({ state, focus, setFocus, openNew, openSettings, run }) {
  if (!state.folders.length) {
    return html`<div class="empty">
      <div class="display">Empty</div>
      Create a project and add its folders. Any folder works; git repos also get a worktree per task and per PR.
      <div class="btns"><button class="key" onClick=${openSettings}><${Icon} name="folder-plus" /> Add a project</button></div>
    </div>`;
  }
  const active = state.tasks.filter((t) => t.status === "active");
  return html`<div class="rail">${state.projects.map((p) => {
    const folders = state.folders.filter((f) => f.projectId === p.id);
    const folderName = Object.fromEntries(folders.map((f) => [f.id, f.name]));
    const tasks = active.filter((t) => t.folderId in folderName);
    return html`<section key=${p.id}>
      <div class="project-head">
        <span class="label">${p.name}</span>
        <span class="faint ellipsis">${folders.map((f) => f.name).join(" · ") || "no folders"}</span>
        <button class="key small ghost" title=${`New task in ${p.name}`} disabled=${!folders.length} onClick=${() => openNew(p.id)}><${Icon} name="plus" /></button>
      </div>
      ${tasks.map((t) => {
        const sessions = state.sessions.filter((s) => s.taskId === t.id);
        const slices = state.slices.filter((x) => x.taskId === t.id);
        const needs = state.approvals.some((a) => sessions.some((s) => s.id === a.sessionId));
        return html`<div key=${t.id} class="task-row" aria-selected=${t.id === focus.taskId} onClick=${() => setFocus({ taskId: t.id })}>
          <span class="id">${t.linear ? t.linear.identifier : "—"}</span>
          <span class="ellipsis">${t.title}</span>
          <span class="lights">${sessions.slice(-6).map((s) => html`<span key=${s.id} class=${cls("sq", s.status)} title=${`${s.title}: ${s.status}`}></span>`)}</span>
          <button class="key small ghost archive" title="Archive this task. Branches and worktrees are kept." onClick=${(e) => {
            e.stopPropagation();
            if (confirm(`Archive "${t.title}"? Branches and worktrees are kept.`)) run(post(`/api/tasks/${t.id}/archive`, { removeWorktrees: false }));
          }}><${Icon} name="archive" /> Archive</button>
          <span class="sub ellipsis">
            ${needs ? html`<span class="waitc">needs approval · </span>` : ""}
            ${folders.length > 1 ? folderName[t.folderId] + " · " : ""}
            ${slices.length ? `${slices.filter((x) => x.prState === "MERGED").length}/${slices.length} PRs merged` : t.branch || "no git"}
          </span>
        </div>`;
      })}
      ${!tasks.length && folders.length > 0 && html`<div class="task-row" onClick=${() => openNew(p.id)}><span class="faint">+</span><span class="faint">new task</span></div>`}
    </section>`;
  })}</div>`;
}
