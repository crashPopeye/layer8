import { Columns2, GitCompare, RefreshCw, Rows2 } from "lucide-react";
import { Fragment, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
import { useFocus } from "@/lib/focus";
import { useAction, useClient, useNode } from "@/lib/react";
import type { Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { diffPath, splitRows, statusLetter, type Diff, type DiffFile, type DiffMode, type Line } from "./model";

/** The focused task's diff against its base, with Commit, Push and Open PR. */
export function ChangesPanel() {
  const focus = useFocus();
  if (!focus.startsWith("task:")) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <GitCompare className="size-5" />
        <p>Focus a task to see its changes.</p>
      </div>
    );
  }
  return <Changes key={focus} addr={focus} />;
}

const modes: [DiffMode, string][] = [
  ["all", "All"],
  ["uncommitted", "Uncommitted"],
  ["committed", "Committed"],
];

function Changes({ addr }: { addr: Addr }) {
  const client = useClient();
  const { node } = useNode(addr, 0);
  const action = useAction(addr);
  const [mode, setMode] = useState<DiffMode>("all");
  const [split, setSplit] = useState(false);
  const [diff, setDiff] = useState<Diff | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [reload, setReload] = useState(0);
  const [message, setMessage] = useState("");

  useEffect(() => {
    let live = true;
    client.getJSON<Diff>(diffPath(addr, mode)).then(
      (d) => live && (setDiff(d), setError(null)),
      (e: Error) => live && setError(e.message),
    );
    return () => {
      live = false;
    };
  }, [client, addr, mode, node, reload]);

  const can = (name: string) => node?.actions.some((a) => a.name === name) ?? false;
  async function commit() {
    if ((await action.run("commit", { message })).ok) setMessage("");
  }

  return (
    <div className="flex h-full flex-col">
      <div className="flex items-center gap-1 border-b px-2 py-1.5">
        {modes.map(([m, label]) => (
          <Button key={m} size="xs" variant={mode === m ? "secondary" : "ghost"} onClick={() => setMode(m)}>
            {label}
          </Button>
        ))}
        {diff && (
          <span className="ml-auto font-mono text-[11px]">
            <span className="text-ok">+{diff.added}</span> <span className="text-bad">−{diff.deleted}</span>
          </span>
        )}
        <Button variant="ghost" size="icon-xs" className={cn(!diff && "ml-auto")} title={split ? "Unified" : "Side by side"} onClick={() => setSplit(!split)}>
          {split ? <Rows2 /> : <Columns2 />}
        </Button>
        <Button variant="ghost" size="icon-xs" title="Refresh" onClick={() => setReload(reload + 1)}>
          <RefreshCw />
        </Button>
      </div>
      <div className="min-h-0 flex-1 overflow-auto">
        {error && <p className="p-2 text-bad">{error}</p>}
        {diff?.files.length === 0 && <p className="p-2 text-muted-foreground">No changes.</p>}
        {diff && diff.files.length > 0 && (
          <ul className="border-b py-1">
            {diff.files.map((f) => (
              <li key={f.path}>
                <button
                  className="flex w-full items-center gap-2 px-2 py-0.5 text-left font-mono text-[11px] hover:bg-muted"
                  onClick={() => document.getElementById(fileId(f))?.scrollIntoView({ block: "start" })}
                >
                  <span className={statusTone[f.status]}>{statusLetter[f.status]}</span>
                  <span className="truncate">{f.path}</span>
                  <span className="ml-auto text-ok">+{f.added}</span>
                  <span className="text-bad">−{f.deleted}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
        {diff?.files.map((f) => <FileDiff key={f.path} file={f} split={split} />)}
      </div>
      <div className="flex flex-col gap-1.5 border-t p-2">
        <textarea
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          placeholder="Commit message"
          rows={2}
          className="resize-none rounded-md border bg-input/30 px-2 py-1 outline-none select-text focus-visible:ring-2 focus-visible:ring-ring/50"
        />
        <div className="flex gap-1.5">
          <Button size="sm" variant="outline" disabled={!can("commit") || !message.trim() || !!action.busy} onClick={commit}>
            Commit
          </Button>
          <Button size="sm" variant="outline" disabled={!can("push") || !!action.busy} onClick={() => action.run("push")}>
            Push
          </Button>
          <Button size="sm" className="ml-auto" disabled={!can("open-pr") || !!action.busy} onClick={() => action.run("open-pr")}>
            Open PR
          </Button>
        </div>
        {action.busy && <p className="text-muted-foreground">{action.busy}…</p>}
        {!action.busy && action.result && (
          <p className={cn("select-text", action.result.ok ? "text-muted-foreground" : "text-bad")}>{action.result.message}</p>
        )}
      </div>
    </div>
  );
}

const statusTone: Record<DiffFile["status"], string> = {
  added: "text-ok",
  deleted: "text-bad",
  modified: "text-wait",
  renamed: "text-primary",
};

const lineTone: Record<Line["kind"], string> = { add: "bg-ok/10", del: "bg-bad/10", ctx: "" };
const sign: Record<Line["kind"], string> = { add: "+", del: "-", ctx: " " };

function fileId(f: DiffFile) {
  return `diff-${f.path}`;
}

function FileDiff({ file, split }: { file: DiffFile; split: boolean }) {
  return (
    <section id={fileId(file)} className="border-b">
      <header className="sticky top-0 z-10 flex items-center gap-2 border-b bg-card px-2 py-1 font-mono text-[11px]">
        <span className={statusTone[file.status]}>{statusLetter[file.status]}</span>
        <span className="truncate">{file.oldPath ? `${file.oldPath} → ${file.path}` : file.path}</span>
      </header>
      {file.binary ? (
        <p className="px-2 py-1 text-muted-foreground">Binary file</p>
      ) : (
        <table className="w-full table-fixed border-collapse font-mono text-[11.5px] leading-5 select-text">
          <colgroup>
            {(split ? ["w-9", "", "w-9", ""] : ["w-9", "w-9", ""]).map((w, i) => (
              <col key={i} className={w} />
            ))}
          </colgroup>
          <tbody>
            {file.hunks.map((h, i) => (
              <Fragment key={i}>
                <tr>
                  <td colSpan={split ? 4 : 3} className="truncate bg-muted/50 px-2 text-muted-foreground">
                    {h.header}
                  </td>
                </tr>
                {split
                  ? splitRows(h.lines).map((r, j) => (
                      <tr key={j}>
                        <Num n={r.left?.old} />
                        <Code line={r.left} />
                        <Num n={r.right?.new} />
                        <Code line={r.right} />
                      </tr>
                    ))
                  : h.lines.map((l, j) => (
                      <tr key={j} className={lineTone[l.kind]}>
                        <Num n={l.old} />
                        <Num n={l.new} />
                        <Code line={l} />
                      </tr>
                    ))}
              </Fragment>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function Num({ n }: { n?: number }) {
  return <td className="pr-1.5 text-right align-top text-muted-foreground/60 select-none">{n || ""}</td>;
}

function Code({ line }: { line?: Line }) {
  return (
    <td className={cn("px-1 align-top break-all whitespace-pre-wrap", line ? lineTone[line.kind] : "bg-muted/30")}>
      {line && (
        <>
          <span className="select-none text-muted-foreground">{sign[line.kind]}</span>
          {line.text}
        </>
      )}
    </td>
  );
}
