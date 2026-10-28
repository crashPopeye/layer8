import type { AppState } from "@layer8/types";
import { home } from "../util";

export function Rail({
  state,
  selected,
  onSelect,
  onNew,
}: {
  state: AppState;
  selected: string | null;
  onSelect: (id: string) => void;
  onNew: () => void;
}) {
  const active = state.tasks.filter((t) => t.status === "active");
  if (!state.workspaces.length) {
    return (
      <div className="empty">
        <div className="display">Empty</div>
        Add a folder as a workspace in settings (<kbd>,</kbd>). Any folder works; git repos also get a worktree per task.
      </div>
    );
  }
  return (
    <>
      {state.workspaces.map((ws) => {
        const tasks = active.filter((t) => t.workspaceId === ws.id);
        return (
          <section key={ws.id}>
            <div className="ws-head">
              <span className="label">{ws.name}</span>
              <span className="faint mono-path" style={{ fontSize: 10.5 }}>
                {home(ws.path)}
              </span>
            </div>
            {tasks.map((t) => {
              const sessions = state.sessions.filter((s) => s.taskId === t.id);
              return (
                <div
                  key={t.id}
                  className="task-row"
                  role="option"
                  aria-selected={t.id === selected}
                  onClick={() => onSelect(t.id)}
                >
                  <span className="id">{t.linear?.identifier ?? "—"}</span>
                  <span className="title">{t.title}</span>
                  <span className="lights">
                    {sessions.slice(-8).map((s) => (
                      <span key={s.id} className={`sq ${s.status}`} title={`${s.kind}: ${s.status}`} />
                    ))}
                  </span>
                  <span className="branch">{t.branch ?? "no git"}</span>
                </div>
              );
            })}
            {!tasks.length && (
              <div className="task-row" onClick={onNew} style={{ color: "var(--ink-3)" }}>
                <span>+</span>
                <span>new task</span>
              </div>
            )}
          </section>
        );
      })}
    </>
  );
}
