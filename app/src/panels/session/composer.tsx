import { isTauri } from "@tauri-apps/api/core";
import { ChevronDown, Paperclip, Square, X } from "lucide-react";
import { useEffect, useLayoutEffect, useMemo, useRef, useState, type ClipboardEvent, type DragEvent, type KeyboardEvent } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { useClient, useNode } from "@/lib/react";
import type { Addr, Client, Summary } from "@/lib/types";
import { cn } from "@/lib/utils";
import { walk } from "@/panels/tasks/tree";
import { useTree } from "@/panels/tasks/use-tree";
import { apply, rank, trigger, type MenuItem } from "./menu";
import type { Image } from "./model";
import { pickSession } from "./target";

interface Capabilities {
  commands?: { name: string; description: string; argumentHint?: string; plugin?: string }[];
  models?: { value: string; displayName: string; efforts?: string[] }[];
}

const capsCache = new Map<string, Promise<Capabilities>>();

function capabilities(client: Client, task: Addr, harness: string): Promise<Capabilities> {
  const key = `${task}\0${harness}`;
  if (!capsCache.has(key)) {
    const p = client.getJSON<Capabilities>(`/api/capabilities?task=${encodeURIComponent(task)}&harness=${harness}`);
    p.catch(() => capsCache.delete(key));
    capsCache.set(key, p);
  }
  return capsCache.get(key)!;
}

/** The items of the `/` menu: every command and skill, with its plugin and description. */
export function commandItems(caps: Capabilities | null): MenuItem[] {
  return (caps?.commands ?? []).map((c) => ({
    label: `/${c.name}`,
    detail: [c.plugin, c.description].filter(Boolean).join(" · "),
    insert: `/${c.name} `,
  }));
}

/** The items of the `@` menu: artifacts, tree nodes and worktree files. */
export function referenceItems(artifactsDir: string, artifacts: Summary[], nodes: Addr[], files: string[]): MenuItem[] {
  return [
    ...artifacts.map((a) => ({ label: a.title, group: "artifact", insert: `@${artifactsDir}/${a.title} ` })),
    ...nodes.map((n) => ({ label: n, group: "node", insert: `@layer8://${n.replace(":", "/")} ` })),
    ...files.map((f) => ({ label: f, group: "file", insert: `@${f} ` })),
  ];
}

const permissionModes: [string, string][] = [
  ["", "default"],
  ["acceptEdits", "accept edits"],
  ["auto", "auto"],
  ["plan", "plan"],
  ["bypassPermissions", "bypass"],
];
const defaultEfforts = ["low", "medium", "high", "xhigh", "max"];

function Chip({
  label,
  title,
  value,
  options,
  onPick,
}: {
  label: string;
  title: string;
  value: string;
  options: [string, string][];
  onPick: (v: string) => void;
}) {
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <button title={title} className="flex items-center gap-1 rounded-full border px-2 py-0.5 font-mono text-[11px] text-muted-foreground hover:text-foreground">
          {label}
          <ChevronDown className="size-3" />
        </button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="start">
        <DropdownMenuRadioGroup value={value} onValueChange={onPick}>
          {options.map(([v, l]) => (
            <DropdownMenuRadioItem key={v || "default"} value={v}>
              {l}
            </DropdownMenuRadioItem>
          ))}
        </DropdownMenuRadioGroup>
      </DropdownMenuContent>
    </DropdownMenu>
  );
}

const readImage = (f: File) =>
  new Promise<Image & { name: string }>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve({ name: f.name, mediaType: f.type, data: String(r.result).split(",")[1] });
    r.onerror = () => reject(r.error);
    r.readAsDataURL(f);
  });

interface Settings {
  harness: string;
  model: string;
  effort: string;
  permissionMode: string;
}

/** Writes to a session of the task or starts a new one: ⏎ sends, ⇧⏎ breaks the line, `/` and `@` complete, Esc twice interrupts. */
export function Composer({ task, session, sessions }: { task: Addr; session: Addr | null; sessions: Summary[] }) {
  const client = useClient();
  const { tree } = useTree();
  const [target, setTarget] = useState<Addr | null>(session);
  useEffect(() => setTarget(session), [session]);
  const taskNode = useNode(task, 0).node;
  const targetNode = useNode(target ?? task, 0).node;
  const live = target && targetNode?.addr === target ? targetNode : null;

  const [text, setText] = useState("");
  const [caret, setCaret] = useState(0);
  const [images, setImages] = useState<(Image & { name: string })[]>([]);
  const [files, setFiles] = useState<string[]>([]);
  const [draft, setDraft] = useState<Settings>({ harness: "claude", model: "", effort: "", permissionMode: "" });
  const [caps, setCaps] = useState<Capabilities | null>(null);
  const [worktree, setWorktree] = useState<string[] | null>(null);
  const [index, setIndex] = useState(0);
  const [dismissed, setDismissed] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const nextCaret = useRef<number | null>(null);
  const lastEsc = useRef(0);

  const f = live?.fields ?? {};
  const settings: Settings = live
    ? { harness: String(f.harness ?? "claude"), model: String(f.model ?? ""), effort: String(f.effort ?? ""), permissionMode: String(f.permissionMode ?? "") }
    : draft;

  useEffect(() => {
    let alive = true;
    capabilities(client, task, settings.harness).then((c) => alive && setCaps(c), () => alive && setCaps({}));
    return () => {
      alive = false;
    };
  }, [client, task, settings.harness]);

  const t = trigger(text, caret);
  const open = t && t.start !== dismissed ? t : null;
  useEffect(() => {
    if (open?.char !== "@" || worktree) return;
    client.getJSON<{ files: string[] }>(`/api/files?task=${encodeURIComponent(task)}`).then(
      (r) => setWorktree(r.files),
      () => setWorktree([]),
    );
  }, [client, task, open?.char, worktree]);

  const items = useMemo(() => {
    if (!open) return [];
    if (open.char === "/") return rank(commandItems(caps), open.query);
    const fields = taskNode?.fields ?? {};
    const nodes = walk(tree).flatMap((i) => (i.kind === "task" ? [i.addr, ...i.sessions.map((s) => s.addr)] : []));
    return rank(referenceItems(String(fields.artifacts ?? ""), (fields.artifactFiles ?? []) as Summary[], nodes, worktree ?? []), open.query);
  }, [open?.char, open?.query, caps, taskNode, tree, worktree]);
  useEffect(() => setIndex(0), [open?.char, open?.query]);

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    el.style.height = "auto";
    el.style.height = `${Math.min(el.scrollHeight, 240)}px`;
    if (nextCaret.current !== null) {
      el.setSelectionRange(nextCaret.current, nextCaret.current);
      nextCaret.current = null;
    }
  }, [text]);

  useEffect(() => {
    if (!isTauri()) return;
    let off: (() => void) | undefined;
    let alive = true;
    import("@tauri-apps/api/webview").then(({ getCurrentWebview }) =>
      getCurrentWebview()
        .onDragDropEvent((e) => {
          const p = e.payload;
          if (p.type === "drop") setFiles((old) => [...new Set([...old, ...p.paths])]);
        })
        .then((u) => (alive ? (off = u) : u())),
    );
    return () => {
      alive = false;
      off?.();
    };
  }, []);

  function edit(value: string, at: number) {
    setText(value);
    setCaret(at);
    if (dismissed !== null && trigger(value, at)?.start !== dismissed) setDismissed(null);
  }

  function pick(item: MenuItem) {
    if (!open) return;
    const next = apply(text, caret, open, item);
    nextCaret.current = next.caret;
    edit(next.text, next.caret);
  }

  async function run(target: Addr, action: string, args?: unknown) {
    const [r] = await client.doOps([{ target, action, args }]);
    if (!r.ok) setError(r.message ?? `${action} failed`);
    return r;
  }

  async function send() {
    const body = text.trim();
    if (busy || (!body && !images.length && !files.length)) return;
    setBusy(true);
    setError(null);
    const say = {
      text: body,
      ...(files.length && { files }),
      ...(images.length && { images: images.map(({ mediaType, data }) => ({ mediaType, data })) }),
    };
    let ok: boolean;
    if (target) ok = (await run(target, "say", say)).ok;
    else {
      const attached = files.length > 0 || images.length > 0;
      const opts = Object.fromEntries(Object.entries(draft).filter(([, v]) => v));
      const started = await run(task, "start-session", attached ? { ...opts, title: body.split("\n")[0].slice(0, 60) || "Session" } : { ...opts, prompt: body });
      ok = started.ok && (!attached || (await run(started.addr, "say", say)).ok);
      if (started.ok) pickSession(started.addr, task);
    }
    setBusy(false);
    if (!ok) return;
    edit("", 0);
    setImages([]);
    setFiles([]);
  }

  function interrupt() {
    if (target && ["running", "waiting"].includes(String(f.status))) run(target, "interrupt");
  }

  function onKeyDown(e: KeyboardEvent<HTMLTextAreaElement>) {
    if (open && items.length) {
      const move = { ArrowDown: 1, ArrowUp: -1 }[e.key];
      if (move) {
        e.preventDefault();
        return setIndex((index + move + items.length) % items.length);
      }
      if (e.key === "Enter" || e.key === "Tab") {
        e.preventDefault();
        return pick(items[index]);
      }
    }
    if (e.key === "Escape") {
      e.preventDefault();
      if (open) return setDismissed(open.start);
      if (Date.now() - lastEsc.current < 600) interrupt();
      lastEsc.current = Date.now();
      return;
    }
    if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
      e.preventDefault();
      send();
    }
  }

  async function addImages(list: FileList | File[]) {
    const imgs = [...list].filter((x) => x.type.startsWith("image/"));
    if (imgs.length) setImages([...images, ...(await Promise.all(imgs.map(readImage)))]);
    return imgs.length;
  }

  function onPaste(e: ClipboardEvent) {
    if ([...e.clipboardData.files].some((x) => x.type.startsWith("image/"))) {
      e.preventDefault();
      addImages(e.clipboardData.files);
    }
  }

  function onDrop(e: DragEvent) {
    if (!e.dataTransfer.files.length) return;
    e.preventDefault();
    addImages(e.dataTransfer.files);
  }

  function change(patch: Partial<Settings>) {
    if (live) run(live.addr, "set", patch);
    else setDraft({ ...draft, ...patch });
  }

  const model = caps?.models?.find((m) => m.value === settings.model);
  const harnesses = ((taskNode?.actions.find((a) => a.name === "start-session")?.args as any)?.properties?.harness?.enum ?? ["claude"]) as string[];
  const running = !!live && ["running", "waiting"].includes(String(f.status));

  return (
    <div className="m-2 shrink-0 rounded-[14px] border bg-background p-2.5" onDragOver={(e) => e.preventDefault()} onDrop={onDrop}>
      {open && (
        <div role="listbox" className="mb-2 max-h-56 overflow-auto rounded-lg border bg-card p-1">
          {items.map((item, i) => (
            <div
              key={item.label + item.group}
              role="option"
              aria-selected={i === index}
              onMouseDown={(e) => {
                e.preventDefault();
                pick(item);
              }}
              className={cn("flex items-center gap-2 rounded-md px-2 py-1", i === index && "bg-muted")}
            >
              <b className="shrink-0 font-mono text-[12px] font-medium text-primary">{item.label}</b>
              <span className="truncate text-[12px] text-muted-foreground">{item.detail ?? item.group}</span>
            </div>
          ))}
          {items.length === 0 && (
            <p className="px-2 py-1 text-[12px] text-muted-foreground">
              {open.char === "/" && !caps ? "Loading commands…" : open.char === "@" && !worktree ? "Loading files…" : "No match"}
            </p>
          )}
        </div>
      )}
      <textarea
        ref={box}
        rows={1}
        value={text}
        placeholder={target ? "Type a message, / for skills, @ for files and artifacts" : "First prompt for the new session, / for skills, @ for files"}
        onChange={(e) => edit(e.target.value, e.target.selectionStart)}
        onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        className="block w-full resize-none bg-transparent outline-none placeholder:text-muted-foreground"
      />
      {(images.length > 0 || files.length > 0) && (
        <div className="mt-2 flex flex-wrap gap-1.5">
          {images.map((img, i) => (
            <span key={i} className="flex items-center gap-1 rounded border pr-1 text-[11px]">
              <img src={`data:${img.mediaType};base64,${img.data}`} alt={img.name} className="h-8 rounded-l" />
              <button title="Remove" onClick={() => setImages(images.filter((_, j) => j !== i))}>
                <X className="size-3" />
              </button>
            </span>
          ))}
          {files.map((p) => (
            <span key={p} className="flex items-center gap-1 rounded border px-1.5 font-mono text-[11px]">
              <Paperclip className="size-3" />
              {p.split("/").pop()}
              <button title="Remove" onClick={() => setFiles(files.filter((x) => x !== p))}>
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}
      {error && <p className="mt-1 text-[12px] text-bad">{error}</p>}
      <div className="mt-2 flex flex-wrap items-center gap-1.5">
        <Chip
          title="Send to"
          label={target ? `→ ${sessions.find((s) => s.addr === target)?.title ?? target}` : "→ new session"}
          value={target ?? ""}
          options={[...sessions.map((s) => [s.addr, s.title] as [string, string]), ["", "New session"]]}
          onPick={(v) => setTarget(v || null)}
        />
        {!target && harnesses.length > 1 && (
          <Chip title="Harness" label={settings.harness} value={settings.harness} options={harnesses.map((h) => [h, h])} onPick={(v) => change({ harness: v })} />
        )}
        <Chip
          title="Model"
          label={`${settings.harness} · ${model?.displayName ?? (settings.model || "default")}`}
          value={settings.model}
          options={[["", "default"], ...(caps?.models ?? []).map((m) => [m.value, m.displayName] as [string, string])]}
          onPick={(v) => change({ model: v })}
        />
        <Chip
          title="Effort"
          label={`effort ${settings.effort || "default"}`}
          value={settings.effort}
          options={[["", "default"], ...(model?.efforts ?? defaultEfforts).map((e) => [e, e] as [string, string])]}
          onPick={(v) => change({ effort: v })}
        />
        <Chip
          title="Permission mode"
          label={permissionModes.find(([v]) => v === settings.permissionMode)?.[1] ?? settings.permissionMode}
          value={settings.permissionMode}
          options={permissionModes}
          onPick={(v) => change({ permissionMode: v })}
        />
        <span className="flex-1" />
        {running && (
          <Button size="xs" variant="outline" title="Interrupt (Esc Esc)" onClick={interrupt}>
            <Square className="fill-current" /> Stop
          </Button>
        )}
        <Button size="xs" disabled={busy} onClick={send}>
          {target ? "Send ↵" : "Start ↵"}
        </Button>
      </div>
    </div>
  );
}
