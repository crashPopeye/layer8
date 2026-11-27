import { useEffect, useLayoutEffect, useRef, useState } from "react";
import type { Lines } from "@/comments/model";

const maxHighlighted = 300_000;

/** Highlights code with shiki in both themes, showing it plain until the grammar loads; marked lines are highlighted. */
export function Code({ text, lang, marks }: { text: string; lang: string; marks?: Lines[] }) {
  const ref = useRef<HTMLDivElement>(null);
  const [html, setHtml] = useState<{ text: string; html: string } | null>(null);
  useEffect(() => {
    if (text.length > maxHighlighted) return;
    let live = true;
    import("shiki")
      .then(({ codeToHtml, bundledLanguages }) =>
        codeToHtml(text, {
          lang: lang in bundledLanguages ? lang : "text",
          themes: { light: "github-light", dark: "github-dark" },
          defaultColor: false,
        }),
      )
      .then(
        (h) => live && setHtml({ text, html: h }),
        () => {},
      );
    return () => {
      live = false;
    };
  }, [text, lang]);
  const highlighted = html?.text === text;
  useLayoutEffect(() => {
    ref.current?.querySelectorAll<HTMLElement>(".line").forEach((el, i) => {
      el.dataset.line = String(i + 1);
      el.classList.toggle("commented", !!marks?.some((m) => m.line <= i + 1 && i + 1 <= m.lineEnd));
    });
  }, [highlighted, html, marks]);
  if (highlighted) return <div ref={ref} className="code select-text" dangerouslySetInnerHTML={{ __html: html.html }} />;
  return (
    <div ref={ref} className="code select-text">
      <pre>
        {text.split("\n").map((line, i) => (
          <span key={i} className="line">
            {line + "\n"}
          </span>
        ))}
      </pre>
    </div>
  );
}
