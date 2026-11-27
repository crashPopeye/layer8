import { html, h, render, useState, useEffect, useRef, useCallback } from "/vendor/preact-htm.js";
import { get, post, put, on, onOpen, startEvents, shell, connect, rememberToken } from "./api.js";
import { cls } from "./util.js";
import { Dock } from "./dock.js";
import { ops, defaultLayout } from "./layout.js";
import { builtins, loadPlugins, act, look } from "./panels.js";
import { sessionDefaults } from "./sessions.js";
import { NewTaskSheet, SettingsSheet, CommandSheet } from "./sheets.js";
import { Icon } from "./icons.js";

const empty = { projects: [], folders: [], tasks: [], slices: [], sessions: [], approvals: [], running: 0, queued: 0, maxRunning: 6, update: {} };

function useAppState() {
  const [state, setState] = useState(empty);
  const [up, setUp] = useState(true);
  const inFlight = useRef(false);
  const again = useRef(false);
  const refresh = useCallback(async () => {
    if (inFlight.current) {
      again.current = true;
      return;
    }
    inFlight.current = true;
    try {
      do {
        again.current = false;
        setState(await get("/api/state"));
        setUp(true);
      } while (again.current);
    } catch {
      setUp(false);
    } finally {
      inFlight.current = false;
    }
  }, []);
  useEffect(() => {
    refresh();
    const offs = [on("layer8:state", refresh), onOpen(refresh)];
    startEvents(setUp);
    // Safety net for anything missed while the stream was down.
    const t = setInterval(refresh, 15000);
    return () => {
      clearInterval(t);
      offs.forEach((off) => off());
    };
  }, [refresh]);
  return { state, up, refresh };
}

/** Updates come from the Tauri shell (signed, verified by Tauri's updater). */
function useUpdate() {
  const [upd, setUpd] = useState(null);
  const check = useCallback(async () => {
    try {
      setUpd(await shell.checkUpdate());
    } catch (e) {
      setUpd({ error: String(e) });
    }
  }, []);
  useEffect(() => {
    if (!shell.available) return;
    check();
    const t = setInterval(check, 6 * 3600 * 1000);
    return () => clearInterval(t);
  }, [check]);
  return [upd, check, setUpd];
}

// External links open in the browser through Go (http/https only).
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[href]");
  if (!a) return;
  const href = a.getAttribute("href");
  if (/^https?:/i.test(href)) {
    e.preventDefault();
    post("/api/open-url", { url: href }).catch(() => {});
  } else if (!href.startsWith("#")) {
    e.preventDefault();
  }
});

const defaultPrefs = () => ({ layout: defaultLayout(), theme: "system", accent: "" });

/** Layout, theme and accent, saved by the daemon so the app and the browser share them. */
function usePrefs() {
  const [prefs, setPrefs] = useState(null);
  const [plugins, setPlugins] = useState({ panels: [], commands: [], errors: [] });
  useEffect(() => {
    get("/api/ui").then(
      async (ui) => {
        setPrefs({ ...defaultPrefs(), ...(ui.prefs || {}) });
        setPlugins(await loadPlugins(ui.plugins));
      },
      () => setPrefs(defaultPrefs()),
    );
  }, []);
  const save = useRef(null);
  const update = useCallback((patch) => {
    setPrefs((p) => {
      const next = { ...p, ...(typeof patch === "function" ? patch(p) : patch) };
      clearTimeout(save.current);
      save.current = setTimeout(() => put("/api/ui", next).catch(() => {}), 400);
      return next;
    });
  }, []);
  return [prefs, update, plugins];
}

const dark = window.matchMedia("(prefers-color-scheme: dark)");
function useTheme(theme, accent) {
  const [, redraw] = useState(0);
  useEffect(() => {
    const on = () => redraw((n) => n + 1);
    dark.addEventListener("change", on);
    return () => dark.removeEventListener("change", on);
  }, []);
  const root = document.documentElement;
  root.dataset.theme = theme === "light" || theme === "dark" ? theme : dark.matches ? "dark" : "light";
  for (const [k, v] of Object.entries({
    "--accent": accent,
    "--accent-soft": accent && `color-mix(in srgb, ${accent} 13%, transparent)`,
    "--accent-line": accent && `color-mix(in srgb, ${accent} 45%, transparent)`,
    "--link": accent,
  })) {
    if (v) root.style.setProperty(k, v);
    else root.style.removeProperty(k);
  }
}

const words = (s) => s.charAt(0).toUpperCase() + s.slice(1).replace(/-/g, " ");

/** ⌘K entries for the verbs of the app and the focused task, PR and session. */
function verbCommands(tree, focus, onResult) {
  if (!tree) return [];
  const ids = [focus.taskId, focus.sliceId, focus.sessionId].filter(Boolean);
  const nodes = [];
  const walk = (n) => {
    if (n.kind === "app" || ids.includes(n.id)) nodes.push(n);
    (n.children || []).forEach(walk);
  };
  walk(tree);
  return nodes.flatMap((n) =>
    (n.verbs || [])
      .filter((v) => v.name !== "show" && (v.args || []).every((a) => a.optional || a.enum))
      .flatMap((v) => {
        const choice = (v.args || []).find((a) => a.enum && !a.optional);
        const run = (args) => () => act(n.ref, v.name, args).then(onResult);
        const group = n.kind === "app" ? "Action" : n.kind === "slice" ? n.title.slice(0, 24) : words(n.kind);
        return choice
          ? choice.enum.map((val) => ({ group, label: `${words(v.name)} ${val}`, title: v.help, run: run({ [choice.name]: val }) }))
          : [{ group, label: words(v.name), title: v.help, run: run({}) }];
      }),
  );
}

function App() {
  const { state, up, refresh } = useAppState();
  const [upd, checkUpdate, setUpd] = useUpdate();
  const [prefs, setPrefs, plugins] = usePrefs();
  useTheme(prefs ? prefs.theme : "system", prefs ? prefs.accent : "");
  const [installing, setInstalling] = useState(false);
  const [focus, setFocus] = useState({ taskId: null, sliceId: "", sessionId: null });
  const [sheet, setSheet] = useState(null);
  const [newIn, setNewIn] = useState(null);
  const [tree, setTree] = useState(null);
  const [error, setError] = useState(null);
  const [note, setNote] = useState(null);
  const openNew = (projectId = null) => {
    setNewIn(projectId);
    setSheet("new");
  };

  useEffect(() => {
    const onKey = (e) => {
      if (!(e.metaKey || e.ctrlKey) || e.shiftKey || e.altKey || e.key.toLowerCase() !== "k") return;
      e.preventDefault();
      setSheet((s) => (s === "search" ? null : "search"));
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
  useEffect(() => {
    if (sheet === "search") look().then(setTree, () => setTree(null));
  }, [sheet]);

  const run = useCallback(async (p) => {
    try {
      setError(null);
      const r = await p;
      refresh();
      return r;
    } catch (e) {
      setError(e.message);
      return undefined;
    }
  }, [refresh]);

  const task = state.tasks.find((t) => t.id === focus.taskId && t.status === "active") || null;
  const slices = task ? state.slices.filter((x) => x.taskId === task.id) : [];
  const slice = slices.find((x) => x.id === focus.sliceId) || null;
  const sessions = task ? state.sessions.filter((s) => s.taskId === task.id && s.sliceId === (slice ? slice.id : "")) : [];
  const session = sessions.find((s) => s.id === focus.sessionId) || null;

  // Keep focus valid, and follow the newest session of the context unless the user picked one.
  useEffect(() => {
    if (focus.taskId && !task) setFocus({ taskId: null, sliceId: "", sessionId: null });
    else if (focus.sliceId && !slice) setFocus({ ...focus, sliceId: "", sessionId: null });
    else if (!session && focus.sessionId !== "__new" && sessions.length) setFocus({ ...focus, sessionId: sessions[sessions.length - 1].id });
  }, [task, slice, session, sessions.length]);
  useEffect(() => {
    put("/api/ui/focus", { taskId: focus.taskId || "", sliceId: focus.sliceId || "", sessionId: session ? session.id : "" }).catch(() => {});
  }, [focus.taskId, focus.sliceId, session && session.id]);

  const layout = prefs && prefs.layout;
  const reveal = (panel) => setPrefs((p) => ({ layout: ops.reveal(p.layout, panel) }));
  const showSession = (s) => {
    setFocus({ taskId: s.taskId, sliceId: s.sliceId || "", sessionId: s.id });
    reveal("sessions");
  };
  const start = async (kind, prompt = "", settings = sessionDefaults()) => {
    const s = await run(post("/api/sessions", { taskId: task.id, sliceId: slice ? slice.id : "", kind, prompt, settings }));
    if (s) showSession(s);
  };
  const open = (target) => run(post("/api/open", { taskId: task.id, sliceId: slice ? slice.id : "", target }));

  // Agents drive the window through act(…, "show" | "notify" | "theme").
  useEffect(() => on("layer8:ui", (ev) => {
    if (ev.verb === "notify") setNote(ev.args.text);
    else if (ev.verb === "theme") setPrefs({ theme: ev.args.mode });
    else if (ev.verb === "show") {
      if (ev.taskId) setFocus({ taskId: ev.taskId, sliceId: ev.sliceId || "", sessionId: ev.sessionId || null });
      const panel = ev.args.panel || (ev.sessionId ? "sessions" : "");
      if (panel) reveal(panel);
    }
  }), []);

  const allPanels = [...builtins, ...plugins.panels];
  const ctx = { state, focus, setFocus, task, slice, slices, sessions, session, run, start, open, reveal, showSession, refresh, openNew, openSettings: () => setSheet("settings"), act, look };
  const panelDef = (id) => {
    if (id === undefined) return allPanels;
    const d = allPanels.find((p) => p.id === id) || { id, title: id, icon: "puzzle", Panel: () => html`<div class="empty">The panel “${id}” is gone: its plugin is missing.</div>` };
    return { ...d, badge: d.badge ? d.badge(ctx) : null };
  };
  const render = (d) => {
    if (d.needsTask && !task) return html`<div class="empty"><div class="display">No task open</div>Pick a task, or start one with <b>New task</b>.</div>`;
    return h(d.Panel, ctx);
  };
  const onActResult = (r) => {
    refresh();
    if (r && r.result && r.result.taskId && r.result.kind) showSession(r.result);
    else if (r && r.text && r.text !== "ok") setNote(r.text);
  };

  const commands = () => [
    { group: "Action", label: "New task", run: () => openNew() },
    ...verbCommands(tree, { ...focus, sessionId: session && session.id }, onActResult).map((c) => ({ ...c, run: () => c.run().catch((e) => setError(e.message)) })),
    ...slices.map((x) => ({ group: "Stack", label: `${x.key}: ${x.title}`, run: () => setFocus({ taskId: task.id, sliceId: x.id }) })),
    ...(slices.length ? [{ group: "Stack", label: "Task: research & plan", run: () => setFocus({ taskId: task.id, sliceId: "" }) }] : []),
    ...state.tasks.filter((t) => t.status === "active").map((t) => ({ group: "Task", label: `${t.linear ? t.linear.identifier + " " : ""}${t.title}`, run: () => setFocus({ taskId: t.id }) })),
    ...allPanels.map((d) => ({ group: "Panel", label: d.title, run: () => reveal(d.id) })),
    ...["system", "dark", "light"].map((m) => ({ group: "Theme", label: words(m), run: () => setPrefs({ theme: m }) })),
    { group: "Layout", label: "Reset layout", run: () => setPrefs({ layout: defaultLayout() }) },
    ...plugins.commands.map((c) => ({ ...c, run: () => Promise.resolve(c.run(ctx)).catch((e) => setError(e.message)) })),
    { group: "Menu", label: "Settings", run: () => setSheet("settings") },
    ...(shell.available ? [{ group: "Action", label: "Check for updates", run: checkUpdate }] : []),
  ];

  const waiting = state.approvals.length;
  return html`
    <div class="frame">
      <header class="top" data-tauri-drag-region>
        <div class="wordmark" data-tauri-drag-region><img src="/logo.png" alt="" class="logo" data-tauri-drag-region /><span data-tauri-drag-region>layer<b>8</b></span></div>
        <button class="omni" onClick=${() => setSheet("search")}><${Icon} name="search" /> <span>Search tasks, actions, panels…</span><kbd>⌘K</kbd></button>
        <div class="grow" data-tauri-drag-region></div>
        <div title="Agents working / limit">
          <span class="label">Agents</span>
          <span class="meter">${Array.from({ length: state.maxRunning }, (_, i) => html`<i class=${i < state.running ? "on" : ""} />`)}</span>
          <span class="dim">${state.running}</span>
          ${state.queued > 0 && html`<span class="faint">+${state.queued} queued</span>`}
        </div>
        <div><span class="label">Approvals</span><span class=${waiting ? "chip signal" : "faint"}>${waiting}</span></div>
        <div title=${up ? "Connected" : "Backend not responding"}>
          <span class=${cls("sq", up ? "idle" : "failed")}></span><span class="label">${/^\d/.test(state.version || "") ? "v" + state.version : state.version || "?"}</span>
        </div>
        <div><button class="key primary" onClick=${() => setSheet("new")}><${Icon} name="plus" /> New task</button></div>
      </header>

      <main class="main">
        ${upd && upd.available &&
        html`<div class="banner">
          <span>layer8 <b>${upd.version}</b> is available.</span>
          ${installing
            ? html`<span class="dim">Downloading and installing… layer8 restarts by itself.</span>`
            : html`<button class="key small primary" onClick=${async () => {
                if (state.running + state.queued > 0 && !confirm("Agents are working. The window restarts, but agents keep running in the background service, which switches to the new version once they finish. Install now?")) return;
                setInstalling(true);
                try {
                  await shell.installUpdate();
                } catch (e) {
                  setInstalling(false);
                  setUpd({ ...upd, error: String(e) });
                }
              }}>Install and restart</button>`}
          ${upd.error && html`<span class="bad">${upd.error}</span>`}
        </div>`}
        ${plugins.errors.length > 0 && html`<div class="banner"><span class="bad">Plugin failed to load:</span> <span class="dim">${plugins.errors.join(" · ")}</span></div>`}
        ${layout ? html`<${Dock} layout=${layout} onChange=${(l) => setPrefs({ layout: l })} panels=${panelDef} render=${render} />` : html`<div class="empty">Loading…</div>`}
      </main>

      <footer class="foot">
        <button class="key small ghost" onClick=${() => setSheet("settings")}><${Icon} name="settings" /> Settings</button>
        ${prefs && html`<span class="seg tiny" title="Theme">
          ${[["system", "sun-moon"], ["light", "sun"], ["dark", "moon"]].map(([m, icon]) => html`<button key=${m} aria-pressed=${prefs.theme === m} title=${words(m)} onClick=${() => setPrefs({ theme: m })}><${Icon} name=${icon} size=${12} /></button>`)}
        </span>
        <label class="accent" title="Accent colour"><${Icon} name="palette" size=${13} />
          <input type="color" value=${prefs.accent || getComputedStyle(document.documentElement).getPropertyValue("--accent").trim() || "#ff8fd2"} onInput=${(e) => setPrefs({ accent: e.target.value })} />
          ${prefs.accent && html`<button class="key small ghost" title="Back to the theme's accent" onClick=${() => setPrefs({ accent: "" })}>reset</button>`}
        </label>`}
        <span class="grow"></span>
        <button class="key small ghost" title="Back to the default panels" onClick=${() => setPrefs({ layout: defaultLayout() })}><${Icon} name="layout-dashboard" /> Reset layout</button>
      </footer>

      ${sheet === "new" &&
      html`<${NewTaskSheet}
        state=${state}
        projectId=${newIn}
        onClose=${() => setSheet(null)}
        onSettings=${() => setSheet("settings")}
        onCreated=${(t) => {
          setSheet(null);
          refresh();
          setFocus({ taskId: t.id, sliceId: "", sessionId: null });
        }}
      />`}
      ${sheet === "settings" && html`<${SettingsSheet} state=${state} run=${run} upd=${upd} onCheckUpdate=${checkUpdate} onClose=${() => setSheet(null)} />`}
      ${sheet === "search" && html`<${CommandSheet} commands=${commands()} onClose=${() => setSheet(null)} />`}
      ${error && html`<div class="toast" onClick=${() => setError(null)}><div class="label bad">error</div>${error}</div>`}
      ${!error && note && html`<div class="toast note" onClick=${() => setNote(null)}><div class="label"><${Icon} name="bell" size=${12} /> layer8</div>${note}</div>`}
    </div>
  `;
}

function Connect({ onDone }) {
  const [token, setToken] = useState("");
  return html`<div class="empty" style="max-width:520px;margin:80px auto">
    <div class="display" style="font-size:28px">layer<span class="signal">8</span></div>
    <p>Paste the daemon token from <code>~/.layer8/token</code> (browser development mode).</p>
    <input class="field" autofocus value=${token} onInput=${(e) => setToken(e.target.value.trim())} />
    <div class="btns"><button class="key primary" disabled=${!token} onClick=${() => { rememberToken(token); onDone(); }}>Connect</button></div>
  </div>`;
}

function Root() {
  const [ready, setReady] = useState(null); // null = connecting, false = needs token, true = ok
  const [error, setError] = useState(null);
  useEffect(() => {
    connect().then((d) => setReady(!!d), (e) => setError(String(e)));
  }, []);
  if (error) return html`<div class="empty"><div class="display">layer8 can't start its service</div><pre class="selectable bad">${error}</pre>
    <p>Details are in <code>~/.layer8/logs/layer8d.log</code>.</p></div>`;
  if (ready === null) return html`<div class="empty">Starting…</div>`;
  if (!ready) return html`<${Connect} onDone=${() => setReady(true)} />`;
  return html`<${App} />`;
}

render(html`<${Root} />`, document.getElementById("root"));
