/** How a file is shown. */
export type ViewerKind = "markdown" | "html" | "image" | "code" | "binary";

const textTypes = /^text\/|[/+](json|xml|javascript|yaml|toml|x-sh)\b/;

/** Picks the viewer for a file from its name and the media type the daemon served it with. */
export function viewerFor(name: string, type: string): ViewerKind {
  const ext = name.includes(".") ? name.split(".").pop()!.toLowerCase() : "";
  if (ext === "md" || ext === "markdown" || type.startsWith("text/markdown")) return "markdown";
  if (ext === "html" || ext === "htm" || type.startsWith("text/html")) return "html";
  if (type.startsWith("image/")) return "image";
  if (textTypes.test(type)) return "code";
  return "binary";
}

/** Returns the highlighting language for a file name: its extension, or a well-known file name. */
export function languageOf(name: string): string {
  const base = name.split("/").pop()!;
  const known: Record<string, string> = { Makefile: "make", Dockerfile: "docker", "go.mod": "go" };
  return known[base] ?? (base.includes(".") ? base.split(".").pop()!.toLowerCase() : "text");
}
