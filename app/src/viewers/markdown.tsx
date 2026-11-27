import { createElement, useEffect, useId, useMemo, useState } from "react";
import Markdown, { type Components, type ExtraProps } from "react-markdown";
import remarkGfm from "remark-gfm";
import type { Lines } from "@/comments/model";
import { cn } from "@/lib/utils";
import { Code } from "./code";

const none: Lines[] = [];

const blocks = ["p", "h1", "h2", "h3", "h4", "h5", "h6", "li", "blockquote", "tr"] as const;

/** Block elements that carry their first source line and are highlighted when a mark overlaps their source lines. */
function marked(marks: Lines[]): Components {
  return Object.fromEntries(
    blocks.map((tag) => [
      tag,
      ({ node, className, ...props }: { className?: string } & ExtraProps) => {
        const start = node?.position?.start.line ?? 0;
        const end = node?.position?.end.line ?? 0;
        const hit = marks.some((m) => m.line <= end && start <= m.lineEnd);
        return createElement(tag, { ...props, "data-line": start || undefined, className: cn(className, hit && "commented") });
      },
    ]),
  );
}

/** Renders a Mermaid diagram, or its source with the parse error. */
function Mermaid({ text }: { text: string }) {
  const id = "m" + useId().replace(/[^a-zA-Z0-9]/g, "");
  const [out, setOut] = useState<{ svg?: string; error?: string }>({});
  useEffect(() => {
    let live = true;
    import("mermaid")
      .then(async ({ default: mermaid }) => {
        mermaid.initialize({
          startOnLoad: false,
          securityLevel: "strict",
          theme: document.documentElement.dataset.theme === "light" ? "default" : "dark",
        });
        return (await mermaid.render(id, text)).svg;
      })
      .then(
        (svg) => live && setOut({ svg }),
        (e: Error) => live && setOut({ error: e.message }),
      );
    return () => {
      live = false;
    };
  }, [id, text]);
  if (out.svg) return <div className="mermaid my-3 flex justify-center" dangerouslySetInnerHTML={{ __html: out.svg }} />;
  return (
    <>
      {out.error && <p className="text-[12px] text-bad">{out.error}</p>}
      <Code text={text} lang="mermaid" />
    </>
  );
}

/** Renders GitHub-flavoured markdown with highlighted code blocks and Mermaid diagrams, highlighting blocks the marks cover. */
export function MarkdownView({ text, marks = none }: { text: string; marks?: Lines[] }) {
  const markedBlocks = useMemo(() => marked(marks), [marks]);
  return (
    <div className="markdown select-text">
      <Markdown
        remarkPlugins={[remarkGfm]}
        components={{
          ...markedBlocks,
          pre: ({ children }) => <>{children}</>,
          code: ({ className, children }) => {
            const lang = /language-([\w-]+)/.exec(className ?? "")?.[1];
            const text = String(children).replace(/\n$/, "");
            if (!lang && !String(children).includes("\n")) return <code>{children}</code>;
            return lang === "mermaid" ? <Mermaid text={text} /> : <Code text={text} lang={lang ?? "text"} />;
          },
        }}
      >
        {text}
      </Markdown>
    </div>
  );
}
