import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { AppState, SessionEvent } from "@layer8/types";
import { Api, discoverDaemon, type DaemonInfo } from "./api";
import { Connect } from "./components/Connect";
import { Inspector } from "./components/Inspector";
import { NewTaskSheet } from "./components/NewTaskSheet";
import { Rail } from "./components/Rail";
import { SettingsSheet } from "./components/SettingsSheet";
import { TaskPane } from "./components/TaskPane";
import { useKeys } from "./util";

const emptyState: AppState = { workspaces: [], tasks: [], sessions: [], approvals: [] };

export type Selection = { taskId: string | null; sessionId: string | null; artifact: string | null };

export function App() {
  const [info, setInfo] = useState<DaemonInfo | null | undefined>(undefined);
  useEffect(() => {
    discoverDaemon().then(setInfo, () => setInfo(null));
  }, []);
  // One client per daemon: a new instance would reconnect the socket.
  const api = useMemo(() => (info ? new Api(info) : null), [info]);
  if (info === undefined) return null;
  if (!api) return <Connect onConnect={setInfo} />;
  return <Workbench api={api} />;
}

function Workbench({ api }: { api: Api }) {
  const [state, setState] = useState<AppState>(emptyState);
  const [up, setUp] = useState(false);
  const [sel, setSel] = useState<Selection>({ taskId: null, sessionId: null, artifact: null });
  const [sheet, setSheet] = useState<"new" | "settings" | null>(null);
  const [error, setError] = useState<string | null>(null);
  // Live events for the open session only; history is fetched when a session is opened.
  const [events, setEvents] = useState<SessionEvent[]>([]);
  const openSession = useRef<string | null>(null);
  openSession.current = sel.sessionId;
  const [cap, setCap] = useState(6);

  useEffect(
    () =>
      api.subscribe((push) => {
        if (push.type === "state") setState(push.state);
        else if (push.event.sessionId === openSession.current) setEvents((prev) => [...prev, push.event]);
      }, setUp),
    [api],
  );

  useEffect(() => {
    if (sheet === null) api.config().then((c) => setCap(c.maxConcurrentSessions), () => {});
  }, [api, sheet]);

  useEffect(() => {
    if (!sel.sessionId) return setEvents([]);
    let live = true;
    setEvents([]);
    api.events(sel.sessionId).then((history) => {
      if (!live) return;
      setEvents((streamed) => {
        const last = history.at(-1)?.id ?? 0;
        return [...history, ...streamed.filter((e) => e.id > last)];
      });
    });
    return () => {
      live = false;
    };
  }, [api, sel.sessionId]);

  const run = useCallback(
    async <T,>(p: Promise<T>): Promise<T | undefined> => {
      try {
        setError(null);
        return await p;
      } catch (e) {
        setError((e as Error).message);
      }
    },
    [],
  );

  const task = state.tasks.find((t) => t.id === sel.taskId) ?? null;
  const taskSessions = useMemo(() => state.sessions.filter((s) => s.taskId === sel.taskId), [state.sessions, sel.taskId]);
  const session = taskSessions.find((s) => s.id === sel.sessionId) ?? null;

  const running = state.sessions.filter((s) => s.status === "running" || s.status === "waiting_approval").length;
  const queued = state.sessions.filter((s) => s.status === "queued").length;
  const waiting = state.approvals.length;

  const selectTask = (taskId: string) => {
    const latest = state.sessions.filter((s) => s.taskId === taskId).at(-1);
    setSel({ taskId, sessionId: latest?.id ?? null, artifact: null });
  };

  useKeys({ n: () => setSheet("new"), ",": () => setSheet("settings") }, sheet === null);

  return (
    <div className="frame">
      <header className="top" data-tauri-drag-region>
        <div className="wordmark" data-tauri-drag-region>
          layer<b>8</b>
        </div>
        <div className="grow" data-tauri-drag-region />
        <div title="Agents running / limit" style={{ gap: 8 }}>
          <span className="label">run</span>
          <Meter used={running} cap={cap} />
          <span className="dim">{running}</span>
          {queued > 0 && <span className="faint">+{queued} queued</span>}
        </div>
        <div style={{ gap: 8 }}>
          <span className="label">approve</span>
          <span className={waiting ? "signal" : "faint"}>{waiting}</span>
        </div>
        <div style={{ gap: 8 }} title={up ? "Connected to layer8d" : "Daemon unreachable, retrying"}>
          <span className={`sq ${up ? "idle" : "failed"}`} />
          <span className="label">{up ? "daemon" : "offline"}</span>
        </div>
        <div style={{ gap: 6 }}>
          <button className="key primary" onClick={() => setSheet("new")}>
            <kbd>N</kbd> New task
          </button>
        </div>
      </header>

      <nav className="rail">
        <Rail state={state} selected={sel.taskId} onSelect={selectTask} onNew={() => setSheet("new")} />
      </nav>

      <main className="main">
        {task ? (
          <TaskPane
            api={api}
            run={run}
            task={task}
            sessions={taskSessions}
            session={session}
            events={events}
            approvals={state.approvals}
            artifact={sel.artifact}
            onSelectSession={(sessionId) => setSel({ ...sel, sessionId, artifact: null })}
            keysEnabled={sheet === null}
          />
        ) : (
          <div className="empty">
            <div className="display">No task open</div>
            Press <kbd>N</kbd> to start one from a Linear ticket.
          </div>
        )}
      </main>

      <aside className="side">
        {task && (
          <Inspector
            api={api}
            run={run}
            task={task}
            session={session}
            artifact={sel.artifact}
            onArtifact={(artifact) => setSel({ ...sel, artifact })}
          />
        )}
      </aside>

      <footer className="foot">
        <span><kbd>N</kbd> new</span>
        <span><kbd>R</kbd>/<kbd>P</kbd>/<kbd>I</kbd> research · plan · implement</span>
        <span><kbd>T</kbd> terminal</span>
        <span><kbd>E</kbd> editor</span>
        <span><kbd>A</kbd>/<kbd>D</kbd> allow · deny</span>
        <span style={{ flex: 1 }} />
        <button className="key" style={{ height: 18, border: "none" }} onClick={() => setSheet("settings")}>
          <kbd>,</kbd> settings
        </button>
      </footer>

      {sheet === "new" && (
        <NewTaskSheet
          api={api}
          workspaces={state.workspaces}
          onClose={() => setSheet(null)}
          onCreated={(t) => {
            setSheet(null);
            setSel({ taskId: t.id, sessionId: null, artifact: null });
          }}
          onSettings={() => setSheet("settings")}
        />
      )}
      {sheet === "settings" && (
        <SettingsSheet api={api} run={run} workspaces={state.workspaces} onClose={() => setSheet(null)} />
      )}
      {error && (
        <div className="toast" onClick={() => setError(null)}>
          <div className="label" style={{ color: "var(--bad)" }}>error</div>
          {error}
        </div>
      )}
    </div>
  );
}

function Meter({ used, cap }: { used: number; cap: number }) {
  return (
    <span className="meter">
      {Array.from({ length: Math.max(cap, used) }, (_, i) => (
        <i key={i} className={i < used ? (i >= cap ? "over" : "on") : ""} />
      ))}
    </span>
  );
}
