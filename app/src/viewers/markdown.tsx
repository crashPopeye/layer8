import { useEffect, useId, useState } from "react";
import Markdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { Code } from "./code";

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

/** Renders GitHub-flavoured markdown with highlighted code blocks and Mermaid diagrams. */
export function MarkdownView({ text }: { text: string }) {
  return (
    <div className="markdown select-text">
      <Markdown
        remarkPlugins={[remarkGfm]}
        components={{
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
