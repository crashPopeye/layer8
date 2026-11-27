import { Check, ChevronDown, ChevronRight, LoaderCircle, X } from "lucide-react";
import { lazy, memo, Suspense, useState } from "react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { Code } from "@/viewers/code";
import { editDiff, toolLabel, toolSummary, type ApprovalItem, type Item, type ToolItem } from "./model";

const MarkdownView = lazy(() => import("@/viewers/markdown").then((m) => ({ default: m.MarkdownView })));

/** How the transcript answers an approval card. */
export type Answer = (id: number, action: "allow" | "deny", always?: boolean) => void;

const kfmt = (n: number) => (n >= 1000 ? `${(n / 1000).toFixed(1)}k` : String(n));

function Markdown({ text }: { text: string }) {
  return (
    <Suspense fallback={<p className="whitespace-pre-wrap">{text}</p>}>
      <MarkdownView text={text} />
    </Suspense>
  );
}

function MiniDiff({ name, input, max }: { name: string; input: unknown; max: number }) {
  const diff = editDiff(name, input);
  if (!diff || !diff.lines.length) return null;
  const shown = diff.lines.slice(0, max);
  return (
    <pre className="my-1 overflow-x-auto rounded-md bg-muted/50 py-1 font-mono text-[11.5px] leading-relaxed">
      {shown.map((l, i) => (
        <span
          key={i}
          className={cn(
            "block px-2",
            l.kind === "add" && "bg-ok/10 text-ok",
            l.kind === "del" && "bg-bad/10 text-bad",
            l.kind === "ctx" && "text-muted-foreground",
          )}
        >
          {l.kind === "add" ? "+ " : l.kind === "del" ? "- " : "  "}
          {l.text}
        </span>
      ))}
      {diff.lines.length > max && <span className="block px-2 text-muted-foreground">… {diff.lines.length - max} more lines</span>}
    </pre>
  );
}

function Tool({ item, answer, pending }: { item: ToolItem; answer: Answer; pending: Set<number> }) {
  const [open, setOpen] = useState(false);
  const diff = editDiff(item.name, item.input);
  const Chevron = open ? ChevronDown : ChevronRight;
  return (
    <div className="my-1.5 border-l-2 pl-2.5 font-mono text-[12px]">
      <button className="flex w-full min-w-0 items-center gap-1.5 text-left text-muted-foreground hover:text-foreground" onClick={() => setOpen(!open)}>
        <Chevron className="size-3 shrink-0" />
        <b className="shrink-0 font-medium text-foreground">{toolLabel(item.name)}</b>
        <span className="truncate">{toolSummary(item.name, item.input)}</span>
        {diff && (
          <span className="shrink-0">
            <span className="text-ok">+{diff.added}</span> <span className="text-bad">−{diff.deleted}</span>
          </span>
        )}
        {item.children.length > 0 && <span className="shrink-0">· {item.children.length} steps</span>}
        <span className="ml-auto shrink-0">
          {!item.result ? (
            <LoaderCircle className="size-3 animate-spin" />
          ) : item.result.isError ? (
            <X className="size-3 text-bad" />
          ) : null}
        </span>
      </button>
      {diff && (item.result || open) && <MiniDiff name={item.name} input={item.input} max={open ? 400 : 12} />}
      {open && (
        <div className="mt-1 grid gap-1">
          {!diff && (
            <div className="max-h-60 overflow-auto rounded-md bg-muted/50">
              <Code text={JSON.stringify(item.input, null, 2)} lang="json" />
            </div>
          )}
          {item.children.length > 0 && <Items items={item.children} answer={answer} pending={pending} />}
          {item.result && (
            <pre className={cn("max-h-72 overflow-auto rounded-md bg-muted/50 px-2 py-1 whitespace-pre-wrap select-text", item.result.isError && "text-bad")}>
              {item.result.text || "(no output)"}
            </pre>
          )}
        </div>
      )}
    </div>
  );
}

function Approval({ item, answer, live }: { item: ApprovalItem; answer: Answer; live: boolean }) {
  if (!live || item.decision !== "pending") {
    const label = item.decision === "allow" ? (item.always ? "Always allowed" : "Allowed") : item.decision === "deny" ? "Denied" : "Expired";
    return (
      <p className="my-1 flex items-center gap-1.5 font-mono text-[12px] text-muted-foreground">
        {item.decision === "allow" ? <Check className="size-3 text-ok" /> : <X className="size-3 text-bad" />}
        {label} {toolLabel(item.tool)}
      </p>
    );
  }
  return (
    <div className="my-2 rounded-lg border border-wait/40 bg-wait/5 p-2.5" role="alertdialog" aria-label={`Allow ${item.tool}?`}>
      <div className="mb-1.5">
        <b>Allow {toolLabel(item.tool)}?</b> {item.description && <span className="text-muted-foreground">{item.description}</span>}
      </div>
      <code className="block max-h-40 overflow-auto font-mono text-[12px] whitespace-pre-wrap">{toolSummary(item.tool, item.input)}</code>
      <MiniDiff name={item.tool} input={item.input} max={20} />
      <div className="mt-2 flex gap-1.5">
        <Button size="xs" onClick={() => answer(item.id, "allow")}>
          Allow <kbd className="font-mono opacity-70">A</kbd>
        </Button>
        <Button size="xs" variant="outline" onClick={() => answer(item.id, "allow", true)}>
          Always
        </Button>
        <Button size="xs" variant="outline" onClick={() => answer(item.id, "deny")}>
          Deny <kbd className="font-mono opacity-70">D</kbd>
        </Button>
      </div>
    </div>
  );
}

function Result({ data }: { data: Record<string, any> }) {
  const u = data.usage ?? {};
  const parts = [
    data.isError ? (data.subtype === "error_during_execution" ? "interrupted" : (data.subtype ?? "error")) : "done",
    data.costUsd != null && `$${Number(data.costUsd).toFixed(2)}`,
    u.input_tokens != null && `${kfmt(u.input_tokens + (u.cache_read_input_tokens ?? 0) + (u.cache_creation_input_tokens ?? 0))} in`,
    u.output_tokens != null && `${kfmt(u.output_tokens)} out`,
    data.durationMs != null && `${Math.round(data.durationMs / 1000)}s`,
    data.numTurns && `${data.numTurns} turns`,
  ].filter(Boolean);
  return (
    <div className={cn("my-2 border-t pt-1 font-mono text-[11px] text-muted-foreground", data.isError && "text-bad")}>
      {parts.join(" · ")}
      {data.isError && data.result && <p className="whitespace-pre-wrap">{data.result}</p>}
    </div>
  );
}

/** One transcript entry. */
export const ItemView = memo(function ItemView({
  item,
  queued,
  answer,
  pending,
}: {
  item: Item;
  queued?: boolean;
  answer: Answer;
  pending: Set<number>;
}) {
  switch (item.kind) {
    case "user":
      return (
        <div className="my-2 rounded-lg bg-muted px-2.5 py-2">
          <p className="whitespace-pre-wrap select-text">{item.text}</p>
          {(item.files?.length || item.images?.length || queued) && (
            <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-muted-foreground">
              {item.images?.map((img, i) => (
                <img key={i} src={`data:${img.mediaType};base64,${img.data}`} alt="" className="h-12 rounded border" />
              ))}
              {item.files?.map((f) => (
                <span key={f} className="rounded border px-1.5 font-mono">
                  {f}
                </span>
              ))}
              {queued && <span className="rounded border border-wait/50 px-1.5 text-wait">queued</span>}
            </div>
          )}
        </div>
      );
    case "text":
      return item.streaming ? <p className="my-1.5 whitespace-pre-wrap select-text">{item.text}</p> : <Markdown text={item.text} />;
    case "thinking":
      return (
        <details open={item.streaming} className="my-1 text-[12px] text-muted-foreground">
          <summary className="cursor-default">Thinking{item.streaming && "…"}</summary>
          <p className="border-l-2 pl-2.5 whitespace-pre-wrap italic select-text">{item.text}</p>
        </details>
      );
    case "tool":
      return <Tool item={item} answer={answer} pending={pending} />;
    case "approval":
      return <Approval item={item} answer={answer} live={pending.has(item.id)} />;
    case "result":
      return <Result data={item.data} />;
    case "error":
      return <p className="my-2 rounded-md bg-bad/10 px-2.5 py-1.5 text-bad whitespace-pre-wrap">{item.message}</p>;
  }
});

/** A list of transcript entries. */
export function Items({ items, answer, pending }: { items: Item[]; answer: Answer; pending: Set<number> }) {
  return items.map((it) => <ItemView key={it.key} item={it} answer={answer} pending={pending} />);
}
