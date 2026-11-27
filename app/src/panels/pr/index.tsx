import { GitPullRequest, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { taskOf, useFocus } from "@/lib/focus";
import { useAction, useNode } from "@/lib/react";
import { openUrl } from "@/lib/tauri";
import type { Addr } from "@/lib/types";
import { cn } from "@/lib/utils";
import { checkTone, prOf, prView, toneText, type Label, type Tone } from "./model";

/** The focused task's pull request: state, checks with failing logs, review threads, and the fix buttons. */
export function PRPanel() {
  const task = taskOf(useFocus());
  if (!task) {
    return (
      <div className="flex h-full flex-col items-center justify-center gap-2 text-muted-foreground">
        <GitPullRequest className="size-5" />
        <p>Focus a task to see its pull request.</p>
      </div>
    );
  }
  return <PullRequest key={task} addr={task} />;
}

const dot: Record<Tone, string> = { ok: "bg-ok", bad: "bg-bad", wait: "bg-wait", primary: "bg-primary", muted: "bg-muted-foreground" };

function Pill({ label }: { label: Label }) {
  return <span className={cn("rounded-full border px-2 py-0.5 text-[11px]", toneText[label.tone])}>{label.text}</span>;
}

function PullRequest({ addr }: { addr: Addr }) {
  const { node, error } = useNode(addr, 0);
  const action = useAction(addr);
  const can = (name: string) => node?.actions.some((a) => a.name === name) ?? false;
  const pr = prOf(node);
  const feedback = action.busy ? (
    <p className="text-muted-foreground">{action.busy}…</p>
  ) : (
    action.result && <p className={cn("select-text", action.result.ok ? "text-muted-foreground" : "text-bad")}>{action.result.message}</p>
  );

  if (error) return <p className="p-3 text-bad">{error.message}</p>;
  if (!node) return null;
  if (!pr) {
    return (
      <div className="flex flex-col items-start gap-2 p-3 text-muted-foreground">
        <p>No pull request yet.</p>
        {can("open-pr") && (
          <Button size="sm" disabled={!!action.busy} onClick={() => action.run("open-pr")}>
            Open PR
          </Button>
        )}
        {feedback}
      </div>
    );
  }

  const v = prView(pr);
  return (
    <div className="flex h-full flex-col gap-4 overflow-auto p-3">
      <div className="flex flex-wrap items-center gap-2">
        <button className="font-semibold hover:underline" onClick={() => openUrl(pr.url)}>
          #{pr.number}
        </button>
        <span className="font-mono text-[11px] text-muted-foreground">→ {pr.base}</span>
        <Pill label={v.state} />
        {v.review && <Pill label={v.review} />}
        {v.merge && <Pill label={v.merge} />}
        <Button variant="ghost" size="icon-xs" className="ml-auto" title="Refresh" disabled={!can("refresh-pr") || !!action.busy} onClick={() => action.run("refresh-pr")}>
          <RefreshCw />
        </Button>
      </div>

      <section className="flex flex-col gap-1">
        <h3 className="flex text-[11px] text-muted-foreground">
          Checks<span className="ml-auto">{v.summary}</span>
        </h3>
        {v.checks.map((c) => (
          <div key={c.name + c.url}>
            <button className="flex w-full items-center gap-2 rounded-md px-1 py-0.5 text-left hover:bg-muted" onClick={() => c.url && openUrl(c.url)}>
              <span className={cn("size-2 shrink-0 rounded-full", dot[checkTone[c.state]])} />
              <span className="truncate">{c.name}</span>
              <span className={cn("ml-auto text-[11px]", toneText[checkTone[c.state]])}>{c.state}</span>
            </button>
            {c.logTail && (
              <pre className="mt-1 max-h-48 overflow-auto rounded-md bg-muted p-2 font-mono text-[11px] leading-4 whitespace-pre-wrap select-text">
                {c.logTail}
              </pre>
            )}
          </div>
        ))}
      </section>

      <section className="flex flex-col gap-1">
        <h3 className="text-[11px] text-muted-foreground">Unresolved threads ({pr.unresolvedThreads})</h3>
        {pr.threads.map((t) => (
          <button key={t.url} className="flex flex-col gap-0.5 rounded-md border p-2 text-left hover:bg-muted" onClick={() => openUrl(t.url)}>
            <span className="font-mono text-[11px] text-muted-foreground">
              {t.path}:{t.line} · {t.author}
              {t.replies > 0 && ` · ${t.replies} ${t.replies === 1 ? "reply" : "replies"}`}
              {t.outdated && " · outdated"}
            </span>
            <span className="line-clamp-3">{t.body}</span>
          </button>
        ))}
      </section>

      {can("start-session") && (
        <div className="flex gap-2">
          <Button size="sm" disabled={!v.canFixCI || !!action.busy} onClick={() => action.run("start-session", { skill: "fix-ci" })}>
            Fix CI
          </Button>
          <Button size="sm" variant="outline" disabled={!v.canAddress || !!action.busy} onClick={() => action.run("start-session", { skill: "address-comments" })}>
            Address comments
          </Button>
        </div>
      )}
      {feedback}
    </div>
  );
}
