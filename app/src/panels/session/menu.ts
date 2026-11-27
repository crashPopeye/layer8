/** What the composer is completing at the caret: a `/` command at the start, or an `@` reference. */
export interface Trigger {
  char: "/" | "@";
  query: string;
  start: number;
}

/** Finds the `/` or `@` token the caret is in, if any. */
export function trigger(text: string, caret: number): Trigger | null {
  const before = text.slice(0, caret);
  const slash = /^\/(\S*)$/.exec(before);
  if (slash) return { char: "/", query: slash[1], start: 0 };
  const at = /(^|\s)@(\S*)$/.exec(before);
  if (at) return { char: "@", query: at[2], start: caret - at[2].length - 1 };
  return null;
}

/** Scores how well query matches text as a subsequence, higher is better, -1 for no match. */
export function fuzzy(query: string, text: string): number {
  const q = query.toLowerCase();
  const t = text.toLowerCase();
  if (!q) return 0;
  const at = t.indexOf(q);
  if (at >= 0) return 1000 - at - t.length / 100;
  let score = 0;
  let j = 0;
  let last = -2;
  for (const ch of q) {
    j = t.indexOf(ch, j);
    if (j < 0) return -1;
    score += j === last + 1 ? 5 : 1;
    if (j === 0 || "/-_:. ".includes(t[j - 1])) score += 3;
    last = j++;
  }
  return score - t.length / 100;
}

/** One entry of a composer menu. */
export interface MenuItem {
  label: string;
  detail?: string;
  group?: string;
  insert: string;
}

/** Keeps the items matching query, best first, at most limit. */
export function rank(items: MenuItem[], query: string, limit = 50): MenuItem[] {
  return items
    .map((item) => ({ item, score: fuzzy(query, item.label) }))
    .filter((s) => s.score >= 0)
    .sort((a, b) => b.score - a.score)
    .slice(0, limit)
    .map((s) => s.item);
}

/** Replaces the trigger token with the picked item's text, returning the new text and caret. */
export function apply(text: string, caret: number, t: Trigger, item: MenuItem): { text: string; caret: number } {
  const head = text.slice(0, t.start) + item.insert;
  return { text: head + text.slice(caret), caret: head.length };
}
