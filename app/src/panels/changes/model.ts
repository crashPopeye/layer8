/** The JSON of `GET /api/diff`, as `internal/diff` writes it. */
export type DiffMode = "all" | "uncommitted" | "committed";

export interface Line {
  kind: "ctx" | "add" | "del";
  old?: number;
  new?: number;
  text: string;
}

export interface Hunk {
  header: string;
  oldStart: number;
  newStart: number;
  lines: Line[];
}

export interface DiffFile {
  path: string;
  oldPath?: string;
  status: "added" | "deleted" | "modified" | "renamed";
  binary?: boolean;
  added: number;
  deleted: number;
  hunks: Hunk[];
}

export interface Diff {
  mode: DiffMode;
  base: string;
  files: DiffFile[];
  added: number;
  deleted: number;
}

/** One row of the side-by-side view: the old line on the left, the new one on the right. */
export interface SplitRow {
  left?: Line;
  right?: Line;
}

/** Pairs a hunk's lines for the side-by-side view: context on both sides, each run of deletions beside the additions that follow it. */
export function splitRows(lines: Line[]): SplitRow[] {
  const rows: SplitRow[] = [];
  let dels: Line[] = [];
  let adds: Line[] = [];
  const flush = () => {
    for (let i = 0; i < Math.max(dels.length, adds.length); i++) rows.push({ left: dels[i], right: adds[i] });
    dels = [];
    adds = [];
  };
  for (const line of lines) {
    if (line.kind === "add") adds.push(line);
    else if (line.kind === "del") {
      if (adds.length) flush();
      dels.push(line);
    } else {
      flush();
      rows.push({ left: line, right: line });
    }
  }
  flush();
  return rows;
}

/** The one-letter badge of a file's status. */
export const statusLetter: Record<DiffFile["status"], string> = { added: "A", deleted: "D", modified: "M", renamed: "R" };

/** The URL of the focused task's diff. */
export function diffPath(task: string, mode: DiffMode): string {
  return `/api/diff?task=${encodeURIComponent(task)}&mode=${mode}`;
}

/** Where a diff comment hangs: deleted lines by their base line number, every other line by its current one. */
export interface Anchor {
  side: "old" | "new";
  line: number;
}

/** The anchor of a diff line. */
export function anchorOf(l: Line): Anchor {
  return l.kind === "del" ? { side: "old", line: l.old! } : { side: "new", line: l.new! };
}

/** The distinct anchors of a side-by-side row. */
export function rowAnchors(r: SplitRow): Anchor[] {
  const list = [r.left, r.right].filter((l): l is Line => !!l).map(anchorOf);
  return list.filter((a, i) => list.findIndex((b) => b.side === a.side && b.line === a.line) === i);
}
