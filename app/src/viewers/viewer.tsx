import { Code2, Eye } from "lucide-react";
import { lazy, Suspense, useEffect, useState } from "react";
import { Button } from "@/components/ui/button";
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

/** Fetches src from the daemon and shows it with the viewer its type calls for; version changes reload it. */
export function Viewer({ src, name, version }: { src: string; name: string; version?: unknown }) {
  const client = useClient();
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
      <div className="absolute inset-0 overflow-auto">
        {source && toggles ? (
          <Code text={file.text} lang={file.kind === "html" ? "html" : "markdown"} />
        ) : file.kind === "markdown" ? (
          <Suspense>
            <MarkdownView text={file.text} />
          </Suspense>
        ) : file.kind === "html" ? (
          <iframe title={name} sandbox="allow-scripts" srcDoc={file.text} className="block size-full border-0 bg-white" />
        ) : file.kind === "image" ? (
          <img src={file.url} alt={name} className="m-auto max-w-full p-3" />
        ) : file.kind === "code" ? (
          <Code text={file.text} lang={languageOf(name)} />
        ) : (
          <p className="p-3 text-muted-foreground">Binary file, {file.size} bytes.</p>
        )}
      </div>
    </div>
  );
}
