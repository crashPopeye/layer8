import { ExternalLink } from "lucide-react";
import { useState, type FormEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input, NativeSelect } from "@/components/ui/input";
import { setFocus } from "@/lib/focus";
import { useAction, useClient, useNode } from "@/lib/react";
import { openUrl } from "@/lib/tauri";
import type { Result, Summary } from "@/lib/types";
import { cn } from "@/lib/utils";
import { issueId, issueTitle } from "./model";

function Feedback({ busy, result }: { busy: string | null; result: Result | null }) {
  if (busy) return <p className="text-muted-foreground">{busy}…</p>;
  if (!result) return null;
  return <p className={cn("select-text", result.ok ? "text-muted-foreground" : "text-bad")}>{result.message}</p>;
}

function IssueRow({ issue, onPick }: { issue: Summary; onPick: (id: string) => void }) {
  const client = useClient();
  const open = () => client.getNode(issue.addr, 0).then((n) => openUrl(String(n.fields?.url)));
  return (
    <div className="flex items-center gap-2 rounded-md px-1 py-0.5 hover:bg-muted">
      <button className="flex min-w-0 flex-1 items-center gap-2 text-left" title="Create a task from this issue" onClick={() => onPick(issueId(issue))}>
        <span className="shrink-0 font-mono text-[11px] text-muted-foreground">{issueId(issue)}</span>
        <span className="truncate">{issueTitle(issue)}</span>
      </button>
      <span className="shrink-0 text-[11px] text-muted-foreground">{issue.status}</span>
      <Button variant="ghost" size="icon-xs" title="Open in Linear" onClick={open}>
        <ExternalLink />
      </Button>
    </div>
  );
}

/** The connected user's open issues, or the results of a search; picking one calls onPick with its identifier. */
export function IssuePicker({ onPick }: { onPick: (id: string) => void }) {
  const { node, error } = useNode("linear", 1);
  const search = useAction("linear");
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<Summary[] | null>(null);

  if (error) return <p className="text-bad">{error.message}</p>;
  if (!node) return null;
  if (node.fields?.connected !== true) return <p className="text-muted-foreground">Linear is not connected: connect it in the Linear panel.</p>;

  async function submit(e: FormEvent) {
    e.preventDefault();
    if (!query.trim()) return setHits(null);
    const r = await search.run("search", { query });
    setHits(r.ok ? (r.data as Summary[]) : []);
  }

  const issues = hits ?? node.children.filter((c) => c.kind === "linear");
  return (
    <div className="flex flex-col gap-1">
      <form onSubmit={submit}>
        <Input value={query} placeholder="Search issues ↵" aria-label="Search Linear issues" onChange={(e) => setQuery(e.target.value)} />
      </form>
      <h3 className="mt-1 text-[11px] text-muted-foreground">{hits ? `Results for “${query}”` : "My issues"}</h3>
      {issues.map((s) => (
        <IssueRow key={s.addr} issue={s} onPick={onPick} />
      ))}
      {issues.length === 0 && !search.busy && <p className="text-muted-foreground">No issues.</p>}
      <Feedback busy={search.busy} result={search.result?.ok ? null : search.result} />
    </div>
  );
}

function Connect({ oauth }: { oauth: boolean }) {
  const linear = useAction("linear");
  const [key, setKey] = useState("");
  async function signIn() {
    const r = await linear.run("connect-oauth");
    if (r.ok) openUrl((r.data as { url: string }).url);
  }
  return (
    <div className="flex flex-col gap-2 p-3">
      <p className="text-muted-foreground">Connect Linear with a personal API key (Linear → Settings → Security & access).</p>
      <form
        className="flex gap-2"
        onSubmit={(e) => {
          e.preventDefault();
          linear.run("connect", { apiKey: key });
        }}
      >
        <Input type="password" value={key} placeholder="lin_api_…" aria-label="Linear API key" onChange={(e) => setKey(e.target.value)} />
        <Button type="submit" size="sm" disabled={!key || !!linear.busy}>
          Connect
        </Button>
      </form>
      {oauth && (
        <Button variant="outline" size="sm" disabled={!!linear.busy} onClick={signIn}>
          Sign in with Linear
        </Button>
      )}
      <Feedback busy={linear.busy} result={linear.result} />
    </div>
  );
}

/** Linear: connect, list my issues, search, and create a task from an issue in a chosen project. */
export function LinearPanel() {
  const { node, error } = useNode("linear", 0);
  const projects = (useNode("app", 1).node?.children ?? []).filter((c) => c.kind === "project");
  const [chosen, setChosen] = useState("");
  const project = chosen || projects[0]?.addr || "";
  const create = useAction(project);
  const linear = useAction("linear");

  if (error) return <p className="p-3 text-bad">{error.message}</p>;
  if (!node) return null;
  if (node.fields?.connected !== true) return <Connect oauth={node.actions.some((a) => a.name === "connect-oauth")} />;

  async function createTask(issue: string) {
    const r = await create.run("create-task-from-linear", { issue });
    if (r.ok) setFocus(r.addr);
  }

  const viewer = node.fields?.viewer as { name: string } | undefined;
  return (
    <div className="flex h-full flex-col gap-3 overflow-auto p-3">
      <div className="flex items-center gap-2 text-muted-foreground">
        <span className="truncate">Linear{viewer && ` · ${viewer.name}`}</span>
        <Button variant="ghost" size="xs" className="ml-auto" disabled={!!linear.busy} onClick={() => linear.run("disconnect")}>
          Disconnect
        </Button>
      </div>
      {node.fields?.error ? <p className="text-bad">{String(node.fields.error)}</p> : null}
      <label className="grid gap-1 text-[12px] text-muted-foreground">
        Create tasks in
        <NativeSelect value={project} aria-label="Project" onChange={(e) => setChosen(e.target.value)}>
          {projects.map((p) => (
            <option key={p.addr} value={p.addr}>
              {p.title}
            </option>
          ))}
        </NativeSelect>
      </label>
      {project ? <IssuePicker onPick={createTask} /> : <p className="text-muted-foreground">Add a project first.</p>}
      <Feedback busy={create.busy ?? linear.busy} result={create.result ?? linear.result} />
    </div>
  );
}
