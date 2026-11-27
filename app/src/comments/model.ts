import type { Addr, Node, Summary } from "@/lib/types";

/** One comment as `internal/comments` puts it in a node's `comments` field. */
export interface Comment {
  addr: Addr;
  target: "artifact" | "diff";
  task: Addr;
  artifact?: Addr;
  path: string;
  line?: number;
  lineEnd?: number;
  side?: "old" | "new";
  quote?: string;
  body: string;
  status: "open" | "sent" | "resolved";
  reply?: string;
  author: string;
  resolvedBy?: string;
  sentTo?: string;
  createdAt: string;
}

/** A source line range, both ends included and counted from 1. */
export interface Lines {
  line: number;
  lineEnd: number;
}

/** The comments a node lists in its `comments` field. */
export function commentsOf(node: Node | null | undefined): Comment[] {
  return (node?.fields?.comments as Comment[] | undefined) ?? [];
}

/** Whether a comment covers the given line. */
export function covers(c: Comment, line: number): boolean {
  return !!c.line && c.line <= line && line <= (c.lineEnd || c.line);
}

/** The line ranges of the comments still to address. */
export function marksOf(comments: Comment[]): Lines[] {
  return comments.filter((c) => c.line && c.status !== "resolved").map((c) => ({ line: c.line!, lineEnd: c.lineEnd || c.line! }));
}

/** The session send-comments goes to by default: the last one that is neither done nor failed. */
export function liveSession(children: Summary[]): Summary | undefined {
  return [...children].reverse().find((c) => c.kind === "session" && c.status !== "done" && c.status !== "failed");
}

/** A short label for where a comment points, such as `L12-14`. */
export function lineLabel(c: Pick<Comment, "line" | "lineEnd">): string {
  if (!c.line) return "";
  return c.lineEnd && c.lineEnd > c.line ? `L${c.line}-${c.lineEnd}` : `L${c.line}`;
}

const markup = /(?<=^|\n)[ \t]*(?:\d+[.)]|[-+*])[ \t]+(?:\[[ xX]\][ \t]+)?|<[^>\n]*>|\]\([^)\s]*\)|[*_`#>|~[\]\\]/y;

/** Text with runs of whitespace collapsed to one space, and markup dropped when strip is set, keeping each character's source offset. */
function normalize(text: string, strip: boolean): { text: string; at: number[] } {
  let out = "";
  const at: number[] = [];
  let space = true;
  for (let i = 0; i < text.length; ) {
    if (strip) {
      markup.lastIndex = i;
      const m = markup.exec(text);
      if (m) {
        i += m[0].length;
        continue;
      }
    }
    if (/\s/.test(text[i])) {
      if (!space) (out += " "), at.push(i);
      space = true;
    } else {
      out += text[i];
      at.push(i);
      space = false;
    }
    i++;
  }
  return { text: out.trimEnd(), at };
}

function lineAt(source: string, offset: number): number {
  let n = 1;
  for (let i = 0; i < offset; i++) if (source[i] === "\n") n++;
  return n;
}

/**
 * Finds the source lines a selection quotes: first comparing text with whitespace collapsed,
 * then also ignoring markdown and HTML markup, so text selected in a rendered view maps back.
 * Among several matches it picks the one starting closest to the near line.
 */
export function locate(source: string, quote: string, near = 1): Lines | null {
  for (const strip of [false, true]) {
    const q = normalize(quote, strip).text;
    if (!q) continue;
    const s = normalize(source, strip);
    let best: Lines | null = null;
    for (let i = s.text.indexOf(q); i >= 0; i = s.text.indexOf(q, i + 1)) {
      const lines = { line: lineAt(source, s.at[i]), lineEnd: lineAt(source, s.at[i + q.length - 1]) };
      if (!best || Math.abs(lines.line - near) < Math.abs(best.line - near)) best = lines;
    }
    if (best) return best;
  }
  return null;
}
