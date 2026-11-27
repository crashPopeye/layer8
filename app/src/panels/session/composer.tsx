import { isTauri } from "@tauri-apps/api/core";
import { ArrowUp, AtSign, ChevronDown, ImagePlus, Paperclip, Square, X } from "lucide-react";
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
import { ModelControl, type Model, type Settings } from "./settings";
import { pickSession } from "./target";
import { BranchChip } from "./worktree";

interface Capabilities {
  commands?: { name: string; description: string; argumentHint?: string; plugin?: string }[];
  models?: Model[];
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

const readImage = (f: File) =>
  new Promise<Image & { name: string }>((resolve, reject) => {
    const r = new FileReader();
    r.onload = () => resolve({ name: f.name, mediaType: f.type, data: String(r.result).split(",")[1] });
    r.onerror = () => reject(r.error);
    r.readAsDataURL(f);
  });

/** A sub-task name from the first words of a prompt. */
export function subtaskName(prompt: string): string {
  const words = prompt.toLowerCase().match(/[a-z0-9]+/g) ?? [];
  return words.slice(0, 4).join("-") || "session";
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
  const [fresh, setFresh] = useState(false);
  const [caps, setCaps] = useState<Capabilities | null>(null);
  const [worktree, setWorktree] = useState<string[] | null>(null);
  const [index, setIndex] = useState(0);
  const [dismissed, setDismissed] = useState<number | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const box = useRef<HTMLTextAreaElement>(null);
  const picker = useRef<HTMLInputElement>(null);
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
      const home = fresh ? await run(task, "create-subtask", { name: subtaskName(body) }) : { ok: true, addr: task };
      const attached = files.length > 0 || images.length > 0;
      const opts = Object.fromEntries(Object.entries(draft).filter(([, v]) => v));
      const started = home.ok
        ? await run(home.addr, "start-session", attached ? { ...opts, title: body.split("\n")[0].slice(0, 60) || "Session" } : { ...opts, prompt: body })
        : home;
      ok = started.ok && (!attached || (await run(started.addr, "say", say)).ok);
      if (started.ok) pickSession(started.addr, home.addr);
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


  function mention() {
    const head = text.slice(0, caret);
    const next = head + (head && !/\s$/.test(head) ? " @" : "@");
    nextCaret.current = next.length;
    edit(next + text.slice(caret), next.length);
    box.current?.focus();
  }

  const harnesses = ((taskNode?.actions.find((a) => a.name === "start-session")?.args as any)?.properties?.harness?.enum ?? ["claude"]) as string[];
  const running = !!live && ["running", "waiting"].includes(String(f.status));
  const empty = !text.trim() && !images.length && !files.length;
  const targetTitle = target ? (sessions.find((s) => s.addr === target)?.title ?? target) : "New session";

  return (
    <div
      className="mx-3 mt-1 mb-3 shrink-0 rounded-2xl border bg-card shadow-sm transition-[border-color,box-shadow] focus-within:border-ring/50 focus-within:ring-[3px] focus-within:ring-ring/15"
      onDragOver={(e) => e.preventDefault()}
      onDrop={onDrop}
    >
      {open && (
        <div role="listbox" className="max-h-56 overflow-auto border-b p-1">
          {items.map((item, i) => (
            <div
              key={item.label + item.group}
              role="option"
              aria-selected={i === index}
              onMouseDown={(e) => {
                e.preventDefault();
                pick(item);
              }}
              className={cn("flex items-center gap-2 rounded-lg px-2 py-1", i === index && "bg-accent")}
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
        aria-label="Message"
        placeholder={target ? "Message the agent — / for skills, @ for files" : "Describe the task for a new session — / for skills, @ for files"}
        onChange={(e) => edit(e.target.value, e.target.selectionStart)}
        onSelect={(e) => setCaret(e.currentTarget.selectionStart)}
        onKeyDown={onKeyDown}
        onPaste={onPaste}
        className="block max-h-60 min-h-11 w-full resize-none bg-transparent px-3.5 pt-3 pb-1 leading-relaxed outline-none placeholder:text-muted-foreground/70"
      />
      {(images.length > 0 || files.length > 0) && (
        <div className="flex flex-wrap gap-1.5 px-3 pt-1 pb-1">
          {images.map((img, i) => (
            <span key={i} className="group relative">
              <img src={`data:${img.mediaType};base64,${img.data}`} alt={img.name} title={img.name} className="size-12 rounded-lg border object-cover" />
              <button
                title="Remove"
                onClick={() => setImages(images.filter((_, j) => j !== i))}
                className="absolute -top-1.5 -right-1.5 hidden size-4 items-center justify-center rounded-full border bg-background group-hover:flex"
              >
                <X className="size-2.5" />
              </button>
            </span>
          ))}
          {files.map((p) => (
            <span key={p} title={p} className="flex h-7 items-center gap-1.5 rounded-lg border bg-muted/50 pr-1 pl-2 font-mono text-[11px]">
              <Paperclip className="size-3 text-muted-foreground" />
              {p.split("/").pop()}
              <button title="Remove" onClick={() => setFiles(files.filter((x) => x !== p))} className="rounded p-0.5 text-muted-foreground hover:text-foreground">
                <X className="size-3" />
              </button>
            </span>
          ))}
        </div>
      )}
      {error && <p className="px-3.5 pt-1 text-[12px] text-bad">{error}</p>}
      <div className="flex flex-wrap items-center gap-0.5 px-2 pt-1 pb-2">
        <Button variant="ghost" size="icon-sm" className="size-7 text-muted-foreground" title="Attach images" onClick={() => picker.current?.click()}>
          <ImagePlus className="size-4" />
        </Button>
        <input
          ref={picker}
          type="file"
          accept="image/*"
          multiple
          hidden
          onChange={(e) => {
            if (e.target.files) addImages(e.target.files);
            e.target.value = "";
          }}
        />
        <Button variant="ghost" size="icon-sm" className="size-7 text-muted-foreground" title="Mention a file or artifact (@)" onClick={mention}>
          <AtSign className="size-4" />
        </Button>
        <span className="mx-1 h-4 w-px bg-border" />
        <ModelControl settings={settings} models={caps?.models ?? []} harnesses={target ? [] : harnesses} onChange={change} />
        {taskNode && (
          <BranchChip task={taskNode} fresh={target ? null : fresh} onFresh={setFresh} onOpen={(where) => run(task, "open", { target: where })} />
        )}
        <span className="ml-auto flex items-center gap-1">
          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <button
                type="button"
                title="Send to"
                className="flex h-7 max-w-44 items-center gap-1 rounded-md px-2 text-[12px] text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 data-[state=open]:bg-accent"
              >
                <span className="truncate">{targetTitle}</span>
                <ChevronDown className="size-3 shrink-0 opacity-60" />
              </button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" side="top">
              <DropdownMenuRadioGroup value={target ?? ""} onValueChange={(v) => setTarget(v || null)}>
                {sessions.map((s) => (
                  <DropdownMenuRadioItem key={s.addr} value={s.addr}>
                    {s.title}
                  </DropdownMenuRadioItem>
                ))}
                <DropdownMenuRadioItem value="">New session</DropdownMenuRadioItem>
              </DropdownMenuRadioGroup>
            </DropdownMenuContent>
          </DropdownMenu>
          {running && (
            <Button variant="outline" size="icon-sm" className="size-7 rounded-full" title="Interrupt (Esc Esc)" onClick={interrupt}>
              <Square className="size-3 fill-current" />
            </Button>
          )}
          <Button size="icon-sm" className="size-7 rounded-full" disabled={busy || empty} title={target ? "Send (⏎)" : "Start session (⏎)"} onClick={send}>
            <ArrowUp className="size-4" />
          </Button>
        </span>
      </div>
    </div>
  );
}
