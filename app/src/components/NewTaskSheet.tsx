import { useEffect, useMemo, useRef, useState } from "react";
import type { LinearIssueSummary, Task, Workspace } from "@layer8/types";
import type { Api } from "../api";

const lastWs = {
  get: () => {
    try {
      return localStorage.getItem("layer8.lastWorkspace");
    } catch {
      return null;
    }
  },
  set: (id: string) => {
    try {
      localStorage.setItem("layer8.lastWorkspace", id);
    } catch {
      // Not worth surfacing: only the default selection is lost.
    }
  },
};

/** Ticket-first task creation: pick a Linear issue, Enter creates the worktree. */
export function NewTaskSheet(props: {
  api: Api;
  workspaces: Workspace[];
  onClose: () => void;
  onCreated: (t: Task) => void;
  onSettings: () => void;
}) {
  const { api, workspaces } = props;
  const [wsId, setWsId] = useState(() => {
    const saved = lastWs.get();
    return workspaces.some((w) => w.id === saved) ? saved! : workspaces[0]?.id;
  });
  const [q, setQ] = useState("");
  const [issues, setIssues] = useState<LinearIssueSummary[] | null>(null);
  const [linearError, setLinearError] = useState<string | null>(null);
  const [cursor, setCursor] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const list = useRef<HTMLDivElement>(null);

  useEffect(() => {
    let live = true;
    const t = setTimeout(
      () => {
        api.linearIssues(q.trim()).then(
          (r) => {
            if (!live) return;
            setIssues(r);
            setLinearError(null);
            setCursor(0);
          },
          (e) => live && setLinearError(e.message),
        );
      },
      q ? 220 : 0,
    );
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [api, q]);

  // A typed identifier ("ENG-123") jumps straight to that ticket even if search ranks it lower.
  const shown = useMemo(() => {
    if (!issues) return [];
    const id = q.trim().toUpperCase();
    return [...issues].sort((a, b) => Number(b.identifier === id) - Number(a.identifier === id));
  }, [issues, q]);

  useEffect(() => {
    list.current?.querySelector('[aria-selected="true"]')?.scrollIntoView({ block: "nearest" });
  }, [cursor]);

  const create = async (body: { linearIssue?: string; title?: string }) => {
    if (!wsId || busy) return;
    setBusy(true);
    setError(null);
    lastWs.set(wsId);
    try {
      props.onCreated(await api.createTask({ workspaceId: wsId, ...body }));
    } catch (e) {
      setError((e as Error).message);
      setBusy(false);
    }
  };

  return (
    <>
      <div className="scrim" onClick={props.onClose} />
      <div
        className="sheet"
        onKeyDown={(e) => {
          if (e.key === "Escape") props.onClose();
          else if (e.key === "ArrowDown") setCursor((c) => Math.min(c + 1, shown.length - 1));
          else if (e.key === "ArrowUp") setCursor((c) => Math.max(c - 1, 0));
          else if (e.key === "Enter" && (e.metaKey || e.ctrlKey)) create({ title: q });
          else if (e.key === "Enter" && shown[cursor]) create({ linearIssue: shown[cursor].identifier });
          else return;
          e.preventDefault();
        }}
      >
        <div className="sheet-head">
          <span className="display">New task</span>
          <span style={{ flex: 1 }} />
          <span className="label">in</span>
          {workspaces.length ? (
            <div className="seg">
              {workspaces.map((w) => (
                <button key={w.id} aria-pressed={w.id === wsId} onClick={() => setWsId(w.id)}>
                  {w.name}
                </button>
              ))}
            </div>
          ) : (
            <button className="key" onClick={props.onSettings}>
              Add a workspace first
            </button>
          )}
        </div>
        <input
          className="field search"
          autoFocus
          placeholder="Search Linear, or type ENG-123…"
          value={q}
          onChange={(e) => setQ(e.target.value)}
        />
        <div className="sheet-body" ref={list}>
          {linearError && (
            <div className="empty">
              <div className="signal">{linearError}</div>
              {/not configured/.test(linearError) && (
                <button className="key" style={{ marginTop: 10 }} onClick={props.onSettings}>
                  Set Linear API key
                </button>
              )}
            </div>
          )}
          {!linearError && issues === null && <div className="empty">Loading your tickets…</div>}
          {!linearError && issues?.length === 0 && <div className="empty">No matching tickets.</div>}
          {shown.map((i, n) => (
            <div
              key={i.id}
              className="issue"
              aria-selected={n === cursor}
              onMouseMove={() => setCursor(n)}
              onClick={() => create({ linearIssue: i.identifier })}
            >
              <span className="sq" style={{ background: i.state.color }} />
              <span className="id">{i.identifier}</span>
              <span className="title">{i.title}</span>
              <span className="state">{i.state.name}</span>
            </div>
          ))}
        </div>
        <div className="sheet-head" style={{ borderTop: "1px solid var(--rule)", borderBottom: "none" }}>
          {error ? (
            <span className="signal">{error}</span>
          ) : busy ? (
            <span className="dim">Creating worktree…</span>
          ) : (
            <span className="faint">
              <kbd>↵</kbd> start from ticket · <kbd>⌘↵</kbd> plain task titled “{q || "…"}” · <kbd>esc</kbd> close
            </span>
          )}
        </div>
      </div>
    </>
  );
}
