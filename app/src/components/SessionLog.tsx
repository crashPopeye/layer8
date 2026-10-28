import { useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { SessionEvent } from "@layer8/types";
import { clock, markdown, usd } from "../util";

/** The argument worth showing on a collapsed tool line. */
function headline(name: string, input: Record<string, any>): string {
  if (name === "Bash") return input.command ?? "";
  if (input.file_path) return input.file_path;
  if (input.pattern) return `${input.pattern}${input.path ? `  in ${input.path}` : ""}`;
  if (input.url) return input.url;
  if (input.description) return input.description;
  if (name === "TodoWrite") return `${(input.todos ?? []).length} items`;
  return JSON.stringify(input);
}

export function SessionLog({ events, children }: { events: SessionEvent[]; children?: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);

  const results = useMemo(() => {
    const m = new Map<string, SessionEvent>();
    for (const e of events) if (e.type === "tool_result") m.set(e.data.toolUseId as string, e);
    return m;
  }, [events]);

  useEffect(() => {
    const el = ref.current;
    if (el && pinned.current) el.scrollTop = el.scrollHeight;
  }, [events, children]);

  return (
    <div
      className="log"
      ref={ref}
      onScroll={(e) => {
        const el = e.currentTarget;
        pinned.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
      }}
    >
      {events.map((e) => (
        <Line key={e.id} event={e} result={e.type === "tool_use" ? results.get(e.data.id as string) : undefined} />
      ))}
      {children}
    </div>
  );
}

function Line({ event: e, result }: { event: SessionEvent; result?: SessionEvent }) {
  const [open, setOpen] = useState(false);
  const sub = e.data.parent ? " sub" : "";
  const t = <span className="t">{clock(e.at)}</span>;
  switch (e.type) {
    case "user":
      return (
        <div className="line user">
          {t}
          <span className="g">›</span>
          <div className="body">{String(e.data.text)}</div>
        </div>
      );
    case "text":
      return (
        <div className={`line text${sub}`}>
          {t}
          <span className="g">◆</span>
          <div className="body md" dangerouslySetInnerHTML={{ __html: markdown(String(e.data.text)) }} />
        </div>
      );
    case "tool_use": {
      const input = e.data.input as Record<string, any>;
      const err = result?.data.isError;
      return (
        <div className={`line${sub}`}>
          {t}
          <span className="g">{open ? "▾" : "▸"}</span>
          <div className="body">
            <div className="tool" onClick={() => setOpen(!open)}>
              <span className="name">{String(e.data.name)}</span>
              <span className="arg">{headline(String(e.data.name), input)}</span>
              <span className={`st${err ? " err" : ""}`}>{result ? (err ? "error" : "ok") : "…"}</span>
            </div>
            {open && (
              <div className="tool-detail">
                {JSON.stringify(input, null, 2)}
                {result && `\n\n──── result ────\n${String(result.data.text)}`}
              </div>
            )}
          </div>
        </div>
      );
    }
    case "result": {
      const ok = !!e.data.ok;
      return (
        <div className={`line result${ok ? "" : " bad"}`}>
          {t}
          <span className="g">{ok ? "■" : "✕"}</span>
          <div className="body">
            {ok ? "done" : `ended: ${e.data.subtype}`} · {Math.round(Number(e.data.durationMs) / 1000)}s ·{" "}
            {String(e.data.turns)} turns · {usd(Number(e.data.costUsd ?? 0))}
          </div>
        </div>
      );
    }
    case "error":
      return (
        <div className="line error">
          {t}
          <span className="g">✕</span>
          <div className="body">{String(e.data.message)}</div>
        </div>
      );
    default:
      return null;
  }
}
