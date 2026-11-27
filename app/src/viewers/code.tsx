import { useEffect, useState } from "react";

const maxHighlighted = 300_000;

/** Highlights code with shiki in both themes, showing it plain until the grammar loads. */
export function Code({ text, lang }: { text: string; lang: string }) {
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
  if (html?.text === text) return <div className="code select-text" dangerouslySetInnerHTML={{ __html: html.html }} />;
  return (
    <div className="code select-text">
      <pre>{text}</pre>
    </div>
  );
}
