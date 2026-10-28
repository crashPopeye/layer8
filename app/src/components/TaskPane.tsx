import { useEffect, useRef, useState } from "react";
import type { Approval, Session, SessionEvent, SessionKind, Task } from "@layer8/types";
import type { Api } from "../api";
import { home, markdown, useKeys } from "../util";
import { ApprovalBlock } from "./ApprovalBlock";
import { SessionLog } from "./SessionLog";

type Run = <T>(p: Promise<T>) => Promise<T | undefined>;

const workflows: { kind: SessionKind; key: string; label: string }[] = [
  { kind: "research", key: "R", label: "Research" },
  { kind: "plan", key: "P", label: "Plan" },
  { kind: "implement", key: "I", label: "Implement" },
];

export function TaskPane(props: {
  api: Api;
  run: Run;
  task: Task;
  sessions: Session[];
  session: Session | null;
  events: SessionEvent[];
  approvals: Approval[];
  artifact: string | null;
  onSelectSession: (id: string) => void;
  keysEnabled: boolean;
}) {
  const { api, run, task, sessions, session, events, artifact } = props;
  const pending = props.approvals.filter((a) => a.sessionId === session?.id);

  const start = async (kind: SessionKind, prompt = "") => {
    const s = await run(api.startSession(task.id, kind, prompt));
    if (s) props.onSelectSession(s.id);
  };

  useKeys(
    {
      r: () => start("research"),
      p: () => start("plan"),
      i: () => start("implement"),
      t: () => run(api.open(task.id, "terminal")),
      e: () => run(api.open(task.id, "editor")),
      a: () => pending[0] && run(api.decide(pending[0].id, true)),
      d: () => pending[0] && run(api.decide(pending[0].id, false)),
    },
    props.keysEnabled,
  );

  return (
    <>
      <div className="task-head">
        <div className="display">
          {task.linear ? (
            <a className="signal" href={task.linear.url} target="_blank" rel="noreferrer">
              {task.linear.identifier}
            </a>
          ) : (
            <span className="signal">{task.slug}</span>
          )}
        </div>
        <h1>{task.title}</h1>
        <div className="task-meta">
          {task.branch && (
            <span>
              <span className="faint">branch</span> {task.branch}
            </span>
          )}
          {task.baseRef && (
            <span>
              <span className="faint">from</span> {task.baseRef}
            </span>
          )}
          <span style={{ minWidth: 0 }}>
            <span className="faint">tree</span> <span className="mono-path">{home(task.worktreePath)}</span>
          </span>
        </div>
      </div>

      <div className="actions">
        {workflows.map((w) => (
          <button key={w.kind} className="key" onClick={() => start(w.kind)}>
            <kbd>{w.key}</kbd> {w.label}
          </button>
        ))}
        <span className="sep" />
        <button className="key" onClick={() => run(api.open(task.id, "terminal"))}>
          <kbd>T</kbd> Terminal
        </button>
        <button className="key" onClick={() => run(api.open(task.id, "editor"))}>
          <kbd>E</kbd> Editor
        </button>
      </div>

      {sessions.length > 0 && (
        <div className="tabs" role="tablist">
          {sessions.map((s) => (
            <button
              key={s.id}
              className="tab"
              role="tab"
              aria-selected={s.id === session?.id && !artifact}
              onClick={() => props.onSelectSession(s.id)}
              title={s.title}
            >
              <span className={`sq ${s.status}`} />
              {s.kind === "chat" ? s.title.slice(0, 24) : s.kind}
            </button>
          ))}
        </div>
      )}

      {artifact ? (
        <ArtifactView api={api} task={task} name={artifact} />
      ) : session ? (
        <>
          <SessionLog events={events}>
            {pending.map((a) => (
              <ApprovalBlock key={a.id} approval={a} decide={(allow, msg) => run(api.decide(a.id, allow, msg))} />
            ))}
          </SessionLog>
          <Composer
            busy={session.status === "running" || session.status === "waiting_approval" || session.status === "queued"}
            busyText={session.status === "queued" ? "Queued: waiting for a free agent slot" : undefined}
            placeholder={`Message ${session.kind} session`}
            onSend={(text) => run(api.send(session.id, text))}
            onInterrupt={() => run(api.interrupt(session.id))}
          />
        </>
      ) : (
        <>
          <div className="log">
            <div className="empty">
              <div className="display">Ready</div>
              Start with <kbd>R</kbd> research. Each workflow runs in a fresh context and hands off through the
              artifacts on the right. Or type below for a free-form session.
            </div>
          </div>
          <Composer busy={false} placeholder="Start a chat session in this worktree" onSend={(t) => start("chat", t)} />
        </>
      )}
    </>
  );
}

function Composer(props: {
  busy: boolean;
  busyText?: string;
  placeholder: string;
  onSend: (text: string) => unknown;
  onInterrupt?: () => unknown;
}) {
  const [text, setText] = useState("");
  const ref = useRef<HTMLTextAreaElement>(null);
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${el.scrollHeight}px`;
  }, [text]);
  const send = () => {
    if (!text.trim() || props.busy) return;
    props.onSend(text.trim());
    setText("");
  };
  return (
    <div className="composer">
      <span className="prompt">›</span>
      <textarea
        ref={ref}
        rows={1}
        value={text}
        placeholder={props.busy ? (props.busyText ?? "Agent is working…") : props.placeholder}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            send();
          }
          if (e.key === "Escape") (e.target as HTMLElement).blur();
        }}
      />
      <div className="send">
        {props.busy && props.onInterrupt ? (
          <button className="key danger" onClick={() => props.onInterrupt?.()}>
            ■ Stop
          </button>
        ) : (
          <button className="key" disabled={!text.trim()} onClick={send}>
            ↵ Send
          </button>
        )}
      </div>
    </div>
  );
}

function ArtifactView({ api, task, name }: { api: Api; task: Task; name: string }) {
  const [content, setContent] = useState<string | null>(null);
  useEffect(() => {
    setContent(null);
    api.artifact(task.id, name).then((a) => setContent(a.content), (e) => setContent(`Error: ${e.message}`));
  }, [api, task.id, name]);
  return (
    <div className="log" style={{ padding: 0 }}>
      <div className="doc">
        <div className="label" style={{ marginBottom: 14 }}>
          {home(task.artifactsPath)}/<span className="signal">{name}</span>
        </div>
        {content === null ? (
          <span className="faint">loading…</span>
        ) : name.endsWith(".md") ? (
          <div className="md" dangerouslySetInnerHTML={{ __html: markdown(content) }} />
        ) : (
          <pre>{content}</pre>
        )}
      </div>
    </div>
  );
}
