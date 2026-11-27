import { Code2, Eye } from "lucide-react";
import { lazy, Suspense, useEffect, useRef, useState } from "react";
import { Button } from "@/components/ui/button";
import { locate, type Lines } from "@/comments/model";
import { useClient } from "@/lib/react";
import { Code } from "./code";
import { languageOf, viewerFor, type ViewerKind } from "./kind";

const MarkdownView = lazy(() => import("./markdown").then((m) => ({ default: m.MarkdownView })));

interface Loaded {
  src: string;
  kind: ViewerKind;
  text: string;
  url?: string;
  size: number;
}

/** Text selected in a viewer, with the source lines it maps to and where it is on screen. */
export interface TextSelection {
  quote: string;
  lines: Lines | null;
  rect: { left: number; top: number; bottom: number };
  open?: boolean;
}

const frameSelectionScript = `<script>(()=>{const post=(open)=>{const s=getSelection(),t=String(s),r=t.trim()&&s.rangeCount?s.getRangeAt(0).getBoundingClientRect():null;
parent.postMessage({layer8Selection:{text:t,rect:r&&{left:r.left,top:r.top,bottom:r.bottom},open}},"*")};
addEventListener("mouseup",()=>post(false));
addEventListener("keydown",(e)=>{if(e.key==="c"&&!e.metaKey&&!e.ctrlKey&&!e.altKey&&String(getSelection()).trim())post(true)})})()</script>`;

function readSelection(root: HTMLElement, text: string): TextSelection | null {
  const sel = getSelection();
  const quote = sel?.toString().trim();
  if (!sel || !quote || !sel.rangeCount || !root.contains(sel.anchorNode)) return null;
  const anchor = sel.anchorNode instanceof Element ? sel.anchorNode : sel.anchorNode?.parentElement;
  const near = Number(anchor?.closest("[data-line]")?.getAttribute("data-line")) || 1;
  const r = sel.getRangeAt(0).getBoundingClientRect();
  return { quote, lines: locate(text, quote, near), rect: { left: r.left, top: r.top, bottom: r.bottom } };
}

/**
 * Fetches src from the daemon and shows it with the viewer its type calls for; version changes reload it.
 * Marks highlight source lines, and onSelectText hears about text selected in it.
 */
export function Viewer({
  src,
  name,
  version,
  marks,
  onSelectText,
}: {
  src: string;
  name: string;
  version?: unknown;
  marks?: Lines[];
  onSelectText?: (s: TextSelection | null) => void;
}) {
  const client = useClient();
  const scroller = useRef<HTMLDivElement>(null);
  const frame = useRef<HTMLIFrameElement>(null);
  const [file, setFile] = useState<Loaded | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [source, setSource] = useState(false);

  useEffect(() => {
    let live = true;
    let url: string | undefined;
    client.raw(src).then(
      async (blob) => {
        const kind = viewerFor(name, blob.type);
        const text = kind === "image" || kind === "binary" ? "" : await blob.text();
        if (kind === "image") url = URL.createObjectURL(blob);
        if (live) {
          setFile({ src, kind, text, url, size: blob.size });
          setError(null);
        }
      },
      (e: Error) => live && setError(e.message),
    );
    return () => {
      live = false;
      if (url) URL.revokeObjectURL(url);
    };
  }, [client, src, name, version]);

  useEffect(() => {
    if (!onSelectText || !file) return;
    const onMessage = (e: MessageEvent) => {
      const sel = e.data?.layer8Selection;
      if (!sel || e.source !== frame.current?.contentWindow) return;
      const quote = String(sel.text).trim();
      if (!quote || !sel.rect) return onSelectText(null);
      const at = frame.current!.getBoundingClientRect();
      const rect = { left: at.left + sel.rect.left, top: at.top + sel.rect.top, bottom: at.top + sel.rect.bottom };
      onSelectText({ quote, lines: locate(file.text, quote), rect, open: sel.open });
    };
    window.addEventListener("message", onMessage);
    return () => window.removeEventListener("message", onMessage);
  }, [onSelectText, file]);

  if (error) return <p className="p-3 text-bad">{error}</p>;
  if (!file || file.src !== src) return <p className="p-3 text-muted-foreground">Loading…</p>;
  const toggles = file.kind === "markdown" || file.kind === "html";

  return (
    <div className="relative min-h-0 flex-1">
      {toggles && (
        <Button
          variant="outline"
          size="icon-xs"
          className="absolute top-2 right-4 z-10"
          title={source ? "Show rendered" : "Show source"}
          onClick={() => setSource(!source)}
        >
          {source ? <Eye /> : <Code2 />}
        </Button>
      )}
      <div
        ref={scroller}
        className="absolute inset-0 overflow-auto"
        onMouseUp={() => onSelectText?.(readSelection(scroller.current!, file.text))}
        onKeyUp={(e) => e.shiftKey && onSelectText?.(readSelection(scroller.current!, file.text))}
      >
        {source && toggles ? (
          <Code text={file.text} lang={file.kind === "html" ? "html" : "markdown"} marks={marks} />
        ) : file.kind === "markdown" ? (
          <Suspense>
            <MarkdownView text={file.text} marks={marks} />
          </Suspense>
        ) : file.kind === "html" ? (
          <iframe
            ref={frame}
            title={name}
            sandbox="allow-scripts"
            srcDoc={onSelectText ? file.text + frameSelectionScript : file.text}
            className="block size-full border-0 bg-white"
          />
        ) : file.kind === "image" ? (
          <img src={file.url} alt={name} className="m-auto max-w-full p-3" />
        ) : file.kind === "code" ? (
          <Code text={file.text} lang={languageOf(name)} marks={marks} />
        ) : (
          <p className="p-3 text-muted-foreground">Binary file, {file.size} bytes.</p>
        )}
      </div>
    </div>
  );
}
