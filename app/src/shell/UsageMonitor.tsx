import { useEffect, useState, type ReactNode } from "react";
import { Gauge } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { setFocus } from "@/lib/focus";
import { useNode } from "@/lib/react";
import type { Node } from "@/lib/types";
import { useShownSession } from "@/panels/session/target";
import { cn } from "@/lib/utils";
import {
  contextGauge,
  formatReset,
  formatTokens,
  level,
  limitsByHarness,
  readUsage,
  tokensByTask,
  type HarnessLimits,
} from "./usage";

/** The footer's usage indicator: the focused session's context and each harness's rate-limit windows, with details on click. */
export function UsageMonitor() {
  const { session } = useShownSession();
  return session ? <SessionUsage key={session} addr={session} /> : <UsageButton session={null} />;
}

function SessionUsage({ addr }: { addr: string }) {
  return <UsageButton session={useNode(addr, 0).node} />;
}

function UsageButton({ session }: { session: Node | null }) {
  const { limits, today } = readUsage(useNode("usage", 0).node?.fields);
  const now = useNowSec();
  const harnesses = limitsByHarness(limits, now);
  const gauge = contextGauge(session?.fields);
  const empty = !gauge && harnesses.length === 0;
  return (
    <Popover>
      <PopoverTrigger
        className="flex h-full items-center gap-3 rounded px-1.5 hover:bg-accent hover:text-accent-foreground data-[state=open]:bg-accent"
        title="Usage"
      >
        {empty && (
          <span className="flex items-center gap-1">
            <Gauge className="size-3" /> usage
          </span>
        )}
        {gauge && (
          <span className="flex items-center gap-1.5">
            ctx
            {gauge.percent != null && <Meter percent={gauge.percent} />}
            <span className="tabular-nums">{gauge.percent != null ? `${Math.round(gauge.percent)}%` : formatTokens(gauge.used)}</span>
          </span>
        )}
        {harnesses.map((h) => (
          <span key={h.harness} className="flex items-center gap-1.5">
            {h.harness}
            {h.windows.map((w) => (
              <span key={w.window} className="flex items-center gap-1" title={`${w.window}: ${Math.round(w.percent)}% used`}>
                <span className="text-muted-foreground/70">{w.window === "weekly" ? "wk" : w.window}</span>
                <Meter percent={w.percent} />
              </span>
            ))}
          </span>
        ))}
      </PopoverTrigger>
      <PopoverContent side="top" align="end" className="w-[22rem] space-y-3 p-0 text-xs">
        {session && (
          <Section title="Context" aside={session.title}>
            {gauge ? (
              <Row
                label={gauge.window > 0 ? `${formatTokens(gauge.used)} / ${formatTokens(gauge.window)}` : `${formatTokens(gauge.used)} tokens`}
                percent={gauge.percent}
              />
            ) : (
              <p className="text-muted-foreground">No turn finished yet.</p>
            )}
          </Section>
        )}
        <Limits harnesses={harnesses} now={now} />
        <Today today={tokensByTask(today)} />
      </PopoverContent>
    </Popover>
  );
}

function Limits({ harnesses, now }: { harnesses: HarnessLimits[]; now: number }) {
  if (harnesses.length === 0) {
    return (
      <Section title="Rate limits">
        <p className="text-muted-foreground">No limit data yet. Windows show up once a session reports them.</p>
      </Section>
    );
  }
  return harnesses.map((h) => (
    <Section key={h.harness} title={h.harness} aside={h.at && `as of ${new Date(h.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`}>
      {h.windows.map((w) => (
        <Row
          key={w.window}
          label={w.window}
          percent={w.percent}
          detail={w.resetsAt ? `resets in ${formatReset(w.resetsAt, now)}` : undefined}
        />
      ))}
    </Section>
  ));
}

function Today({ today }: { today: ReturnType<typeof tokensByTask> }) {
  const total = today.reduce((sum, t) => sum + t.total, 0);
  return (
    <Section title="Today" aside={total > 0 && `${formatTokens(total)} tokens`}>
      {today.length === 0 && <p className="text-muted-foreground">No tokens used today.</p>}
      <ul className="max-h-56 space-y-1.5 overflow-auto">
        {today.map((t) => (
          <li key={t.task}>
            <div className="flex justify-between font-medium">
              <span className="truncate">{t.task.replace(/^task:/, "")}</span>
              <span className="tabular-nums">{formatTokens(t.total)}</span>
            </div>
            {t.sessions.map((s) => (
              <button
                key={s.session}
                className="flex w-full justify-between gap-2 rounded px-1 text-muted-foreground hover:bg-accent hover:text-accent-foreground"
                title={`${formatTokens(s.input)} in · ${formatTokens(s.output)} out · ${formatTokens(s.cacheRead)} cache read · ${formatTokens(s.cacheWrite)} cache write`}
                onClick={() => setFocus(s.session, s.task)}
              >
                <span className="truncate">
                  {s.title} · {s.harness}
                </span>
                <span className="tabular-nums">{formatTokens(s.total)}</span>
              </button>
            ))}
          </li>
        ))}
      </ul>
    </Section>
  );
}

function Section({ title, aside, children }: { title: string; aside?: string | false; children: ReactNode }) {
  return (
    <section className="space-y-1.5 border-b px-3 py-2.5 last:border-b-0">
      <header className="flex items-baseline justify-between gap-2">
        <h3 className="font-medium capitalize">{title}</h3>
        {aside && <span className="truncate text-[11px] text-muted-foreground">{aside}</span>}
      </header>
      {children}
    </section>
  );
}

function Row({ label, percent, detail }: { label: string; percent: number | null; detail?: string }) {
  return (
    <div className="grid grid-cols-[5.5rem_1fr_2.5rem] items-center gap-2">
      <span className="truncate text-muted-foreground">{label}</span>
      {percent != null ? <Meter percent={percent} className="w-full" /> : <span />}
      <span className="text-right tabular-nums">{percent != null && `${Math.round(percent)}%`}</span>
      {detail && <span className="col-start-2 col-end-4 text-[11px] text-muted-foreground">{detail}</span>}
    </div>
  );
}

const fill = { ok: "bg-foreground/45", wait: "bg-wait", bad: "bg-bad" };

function Meter({ percent, className }: { percent: number; className?: string }) {
  return (
    <span className={cn("relative block h-1.5 w-7 overflow-hidden rounded-full bg-muted", className)}>
      <span className={cn("absolute inset-y-0 left-0 rounded-full", fill[level(percent)])} style={{ width: `${percent}%` }} />
    </span>
  );
}

function useNowSec(): number {
  const [now, setNow] = useState(() => Date.now() / 1000);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now() / 1000), 60_000);
    return () => clearInterval(id);
  }, []);
  return now;
}
