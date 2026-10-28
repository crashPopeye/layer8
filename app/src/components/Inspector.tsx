import { useEffect, useState } from "react";
import type { Artifact, Session, Task } from "@layer8/types";
import type { Api } from "../api";
import { home, usd } from "../util";

type Run = <T>(p: Promise<T>) => Promise<T | undefined>;

export function Inspector(props: {
  api: Api;
  run: Run;
  task: Task;
  session: Session | null;
  artifact: string | null;
  onArtifact: (name: string | null) => void;
}) {
  const { api, run, task, session } = props;
  const [artifacts, setArtifacts] = useState<Artifact[]>([]);

  // Sessions write artifacts as they go; refresh whenever the open session changes state.
  useEffect(() => {
    api.artifacts(task.id).then(setArtifacts, () => setArtifacts([]));
  }, [api, task.id, session?.status, session?.id]);

  return (
    <>
      <div className="block">
        <div className="label">
          <span>Artifacts</span>
          <span>{artifacts.length}</span>
        </div>
        {artifacts.map((a) => (
          <button
            key={a.name}
            className="list-row"
            aria-selected={props.artifact === a.name}
            onClick={() => props.onArtifact(props.artifact === a.name ? null : a.name)}
          >
            <span className="grow">{a.name}</span>
            <span className="faint">{new Date(a.updatedAt).toLocaleDateString([], { month: "short", day: "numeric" })}</span>
          </button>
        ))}
        {!artifacts.length && <div className="faint">None yet. Research and plan sessions write here.</div>}
      </div>

      {session && (
        <div className="block">
          <div className="label">
            <span>Session</span>
            <span className="dim">{session.status.replace("_", " ")}</span>
          </div>
          <dl className="kv">
            <dt>kind</dt>
            <dd>{session.kind}</dd>
            <dt>cost</dt>
            <dd>{usd(session.costUsd)}</dd>
            <dt>claude id</dt>
            <dd title={session.claudeSessionId ?? ""}>{session.claudeSessionId?.slice(0, 8) ?? "—"}</dd>
          </dl>
          <div style={{ display: "flex", gap: 6, marginTop: 12, flexWrap: "wrap" }}>
            <button
              className="key"
              disabled={!session.claudeSessionId}
              onClick={() => run(api.resumeInTerminal(session.id))}
              title="claude --resume in this task's tmux session"
            >
              ↗ Continue in terminal
            </button>
          </div>
        </div>
      )}

      <div className="block">
        <div className="label">Task</div>
        <dl className="kv">
          {task.linear && (
            <>
              <dt>linear</dt>
              <dd>
                <a href={task.linear.url} target="_blank" rel="noreferrer">
                  {task.linear.identifier} ↗
                </a>
              </dd>
            </>
          )}
          <dt>tmux</dt>
          <dd>{task.tmuxSession ?? "off"}</dd>
          <dt>artifacts</dt>
          <dd title={task.artifactsPath}>{home(task.artifactsPath)}</dd>
          <dt>tree</dt>
          <dd title={task.worktreePath}>{home(task.worktreePath)}</dd>
        </dl>
        <div style={{ display: "flex", gap: 6, marginTop: 12 }}>
          <button
            className="key danger"
            onClick={() => {
              const removeWorktree = task.branch
                ? confirm("Also remove the worktree? The branch is kept either way.")
                : false;
              run(api.archiveTask(task.id, removeWorktree));
            }}
          >
            Archive
          </button>
        </div>
      </div>
    </>
  );
}
