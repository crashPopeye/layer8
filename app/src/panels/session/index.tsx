import { Ellipsis, Plug } from "lucide-react";
import { Popover } from "radix-ui";
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu";
import { taskOf, useFocus } from "@/lib/focus";
import { useClient, useNode } from "@/lib/react";
import type { Addr, Node } from "@/lib/types";
import { cn } from "@/lib/utils";
import { walk } from "@/panels/tasks/tree";
import { useTree } from "@/panels/tasks/use-tree";
import { Composer } from "./composer";
import { empty, isQueued, reduceAll, type SessionEvent, type Transcript } from "./model";
import { latest, pickSession, useDraft } from "./target";
import { ItemView, type Answer } from "./transcript";

const page = 1000;
const window0 = 200;

/** Reads a session's stored events, then follows its live ones, deltas included. */
export function useTranscript(addr: Addr | null): Transcript {
  const client = useClient();
  const [t, setT] = useState<{ addr: Addr | null; t: Transcript }>({ addr, t: empty });
  useEffect(() => {
    if (!addr) return;
    const id8 = addr.slice("session:".length);
    let live = true;
    let loaded = false;
    let cur = empty;
    const buffered: SessionEvent[] = [];
    const apply = (events: SessionEvent[]) => {
      cur = reduceAll(events, cur);
      if (live) setT({ addr, t: cur });
    };
    const off = client.subscribe((e) => {
      if (e.type !== "session" || e.data.session !== id8) return;
      if (loaded) apply(e.data.events);
      else buffered.push(...e.data.events.filter((x) => !x.partial));
    });
    (async () => {
      for (let after = 0; ; ) {
        const { events } = await client.getJSON<{ events: SessionEvent[] }>(`/api/sessions/${id8}/events?after=${after}&limit=${page}`);
        cur = reduceAll(events, cur);
        if (events.length < page) break;
        after = events.at(-1)!.id!;
      }
    })()
      .catch(() => {})
      .finally(() => {
        loaded = true;
        apply(buffered);
      });
    return () => {
      live = false;
      off();
    };
  }, [client, addr]);
  return t.addr === addr ? t.t : empty;
}

const statusDot: Record<string, string> = {
  running: "bg-primary shadow-[0_0_0_3px] shadow-primary/20",
  starting: "bg-primary",
  waiting: "bg-wait",
  queued: "border border-wait",
  failed: "bg-bad",
  done: "bg-ok",
};

const mcpDot = (status: string) =>
  status === "connected" ? "bg-ok" : status === "failed" ? "bg-bad" : status === "pending" ? "bg-primary" : "bg-wait";

/** The MCP servers, connectors and plugins the session loaded, from its system/init event. */
function Capabilities({ caps }: { caps: any }) {
  const servers: { name: string; status: string }[] = caps?.mcpServers ?? [];
  const plugins: { name: string }[] = caps?.plugins ?? [];
  if (!servers.length && !plugins.length) return null;
  const worst = servers.find((s) => s.status === "failed") ?? servers.find((s) => s.status !== "connected");
  return (
    <Popover.Root>
      <Popover.Trigger asChild>
        <Button variant="outline" size="xs" className="font-mono text-[11px] text-muted-foreground" title="Loaded MCP servers and plugins">
          <span className={cn("size-1.5 rounded-full", mcpDot(worst?.status ?? "connected"))} />
          {servers.length} MCP · {plugins.length} plugins
        </Button>
      </Popover.Trigger>
      <Popover.Portal>
        <Popover.Content sideOffset={4} align="end" className="z-50 w-72 rounded-md border bg-popover p-2 text-[12px] shadow-md">
          <p className="mb-1 text-[11px] text-muted-foreground">MCP servers and connectors</p>
          {servers.map((s) => (
            <p key={s.name} className="flex items-center gap-2 py-0.5">
              <span className={cn("size-1.5 rounded-full", mcpDot(s.status))} />
              <span className="truncate">{s.name}</span>
              <span className="ml-auto font-mono text-[11px] text-muted-foreground">{s.status}</span>
            </p>
          ))}
          <p className="mt-2 mb-1 text-[11px] text-muted-foreground">Plugins</p>
          {plugins.map((p) => (
            <p key={p.name} className="flex items-center gap-2 py-0.5">
              <Plug className="size-3 text-muted-foreground" />
              {p.name}
            </p>
          ))}
        </Popover.Content>
      </Popover.Portal>
    </Popover.Root>
  );
}

const menuActions = ["interrupt", "fork", "open-terminal", "close"];

function Header({ node, task, onMessage }: { node: Node; task: Addr; onMessage: (m: string | null) => void }) {
  const client = useClient();
  const f = node.fields ?? {};
  const actions = node.actions.filter((a) => menuActions.includes(a.name));
  async function run(action: string) {
    const [r] = await client.doOps([{ target: node.addr, action }]);
    onMessage(r.ok ? null : (r.message ?? `${action} failed`));
    if (r.ok && action === "fork") pickSession(r.addr, task);
  }
  return (
    <div className="flex h-9 shrink-0 items-center gap-2 border-b px-3">
      <span className={cn("size-[7px] shrink-0 rounded-full bg-muted-foreground", statusDot[String(f.status)])} />
      <b className="truncate font-semibold">{node.title}</b>
      <span className="shrink-0 font-mono text-[11px] text-muted-foreground">
        {[f.harness, f.model, f.status, Number(f.costUsd) > 0 && `$${Number(f.costUsd).toFixed(2)}`].filter(Boolean).join(" · ")}
      </span>
      <span className="flex-1" />
      <Capabilities caps={f.capabilities} />
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="icon-xs" title="Session actions">
            <Ellipsis />
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="end">
          {actions.map((a) => (
            <DropdownMenuItem key={a.name} title={a.help} onSelect={() => run(a.name)}>
              {a.name}
            </DropdownMenuItem>
          ))}
          {actions.length === 0 && <DropdownMenuItem disabled>No actions</DropdownMenuItem>}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  );
}

const editable = (t: EventTarget | null) =>
  t instanceof HTMLElement && (t.isContentEditable || ["INPUT", "TEXTAREA", "SELECT"].includes(t.tagName));

function SessionView({ node, task }: { node: Node; task: Addr }) {
  const client = useClient();
  const t = useTranscript(node.addr);
  const [message, setMessage] = useState<string | null>(null);
  const [shown, setShown] = useState(window0);
  const scroller = useRef<HTMLDivElement>(null);
  const stick = useRef(true);
  const status = String(node.fields?.status ?? "");
  const approvals = (node.fields?.approvals ?? []) as { id: number }[];
  const pending = useMemo(() => new Set(approvals.map((a) => a.id)), [approvals.map((a) => a.id).join()]);

  const answer: Answer = useCallback(
    async (id, action, always) => {
      const [r] = await client.doOps([{ target: node.addr, action, args: always ? { approval: id, always } : { approval: id } }]);
      setMessage(r.ok ? null : (r.message ?? `${action} failed`));
    },
    [client, node.addr],
  );

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const key = e.key.toLowerCase();
      if (e.metaKey || e.ctrlKey || e.altKey || editable(e.target) || (key !== "a" && key !== "d") || !approvals.length) return;
      e.preventDefault();
      answer(approvals[0].id, key === "a" ? "allow" : "deny");
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [answer, approvals]);

  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && stick.current) el.scrollTop = el.scrollHeight;
  }, [t]);

  const items = t.items.slice(-shown);
  return (
    <>
      <Header node={node} task={task} onMessage={setMessage} />
      <div
        ref={scroller}
        className="transcript min-h-0 flex-1 overflow-auto px-3 py-2"
        onScroll={(e) => {
          const el = e.currentTarget;
          stick.current = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
        }}
      >
        {t.items.length > shown && (
          <Button variant="ghost" size="xs" className="mx-auto flex" onClick={() => setShown(shown + window0)}>
            Show {Math.min(window0, t.items.length - shown)} earlier
          </Button>
        )}
        {items.map((it) => (
          <ItemView key={it.key} item={it} queued={isQueued(t, it, status)} answer={answer} pending={pending} />
        ))}
        {t.items.length === 0 && <p className="text-muted-foreground">No messages yet.</p>}
        {node.fields?.error ? <p className="my-2 text-bad">{String(node.fields.error)}</p> : null}
      </div>
      {message && <p className="border-t px-3 py-1 text-[12px] text-bad">{message}</p>}
    </>
  );
}

function SessionNode({ addr, task }: { addr: Addr; task: Addr }) {
  const { node, error } = useNode(addr, 0);
  if (error) return <p className="p-3 text-bad">{error.message}</p>;
  if (!node) return <p className="p-3 text-muted-foreground">Loading…</p>;
  return <SessionView node={node} task={task} />;
}

/** The focused session's live transcript, approvals and composer; a new-session composer when the task has none or ⌘T asked for one. */
export function SessionPanel() {
  const focus = useFocus();
  const task = taskOf(focus);
  const draft = useDraft();
  const { tree } = useTree();
  if (!task) return <p className="p-3 text-muted-foreground">Focus a task to see its sessions.</p>;
  const sessions = walk(tree).find((i) => i.addr === task)?.sessions ?? [];
  const session = focus.startsWith("session:") ? focus : draft === task ? null : (latest(sessions)?.addr ?? null);

  return (
    <div className="flex h-full flex-col">
      {session ? (
        <SessionNode key={session} addr={session} task={task} />
      ) : (
        <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-1 text-muted-foreground">
          <p>New session in {task}</p>
          <p className="text-[12px]">Pick harness, model, effort and permissions below, then send the first prompt.</p>
        </div>
      )}
      <Composer key={task} task={task} session={session} sessions={sessions} />
    </div>
  );
}
