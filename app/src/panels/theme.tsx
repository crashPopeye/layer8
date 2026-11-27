import { useEffect, useRef, useState, type ChangeEvent } from "react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Select } from "@/components/ui/select";
import { useClient, useNode } from "@/lib/react";
import {
  colorTokens,
  resolveMode,
  setPreview,
  sharedTokens,
  token,
  usePrefersDark,
  type Theme,
  type ThemeFields,
} from "@/lib/theme";
import { cn } from "@/lib/utils";

const hex = (v: string) => (/^#[0-9a-f]{6}$/i.test(v) ? v : "#000000");

/** The theme editor: pick a theme, mode and accent, edit tokens with a live preview, import, export and save. */
export function ThemePanel() {
  const client = useClient();
  const { node, error: loadError } = useNode("ui:theme", 0);
  const prefersDark = usePrefersDark();
  const [draft, setDraft] = useState<Theme | null>(null);
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const accentTimer = useRef<ReturnType<typeof setTimeout>>(undefined);

  useEffect(() => setPreview(draft), [draft]);
  useEffect(() => () => setPreview(null), []);

  if (!node) return <p className="p-3 text-muted-foreground">{loadError ? "Themes need the layer8 daemon." : "Loading…"}</p>;
  const f = node.fields as unknown as ThemeFields;
  const scheme = resolveMode(f.mode, prefersDark);
  const theme = draft ?? f.themes[f.theme] ?? {};

  const run = async (action: string, args: unknown) => {
    const [r] = await client.doOps([{ target: "ui:theme", action, args }]);
    setError(r.ok ? null : (r.message ?? `${action} failed`));
    return r.ok;
  };
  const part = (t: string) => (sharedTokens.includes(t) ? "shared" : scheme);
  const value = (t: string) => theme[part(t)]?.[t] ?? token(t);
  const edit = (t: string, v: string) => setDraft({ ...theme, [part(t)]: { ...theme[part(t)], [t]: v } });
  const setAccent = (color: string) => {
    clearTimeout(accentTimer.current);
    accentTimer.current = setTimeout(() => run("set-accent", { color }), 250);
  };

  async function importFile(e: ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    try {
      const parsed = JSON.parse(await file.text());
      if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) throw new Error("not a theme object");
      setDraft(parsed as Theme);
      setName(file.name.replace(/\.json$/, "").toLowerCase().replace(/[^a-z0-9-]+/g, "-"));
      setError(null);
    } catch (err) {
      setError(`${file.name}: ${(err as Error).message}`);
    }
  }

  async function save() {
    if (await run("save", { name, tokens: theme })) setDraft(null);
  }

  return (
    <div className="grid h-full content-start gap-4 overflow-auto p-3 text-[12px]">
      <section className="grid grid-cols-[56px_minmax(0,1fr)] items-center gap-2">
        <span className="text-muted-foreground">theme</span>
        <Select
          aria-label="theme"
          value={f.theme}
          options={Object.keys(f.themes)}
          onValueChange={(theme) => {
            setDraft(null);
            run("set", { theme });
          }}
        />
        <span className="text-muted-foreground">mode</span>
        <div className="flex flex-wrap gap-1">
          {(["dark", "light", "system"] as const).map((m) => (
            <Button key={m} size="xs" variant={f.mode === m ? "secondary" : "ghost"} onClick={() => run("set", { mode: m })}>
              {m}
            </Button>
          ))}
        </div>
        <span className="text-muted-foreground">accent</span>
        <div className="flex items-center gap-2">
          <input type="color" aria-label="accent" value={hex(f.accent || token("primary"))} onChange={(e) => setAccent(e.target.value)} />
          <code className="truncate font-mono">{f.accent || "theme primary"}</code>
          {f.accent && (
            <Button size="xs" variant="ghost" onClick={() => run("set-accent", { color: "" })}>
              clear
            </Button>
          )}
        </div>
      </section>

      <section className="grid gap-1.5">
        <h3 className={cn("font-medium", draft && "text-primary")}>
          Tokens · {scheme}
          {draft && " · unsaved preview"}
        </h3>
        {colorTokens.map((t) => (
          <label key={t} className="grid grid-cols-[minmax(0,110px)_28px_minmax(0,1fr)] items-center gap-2">
            <span className="truncate text-muted-foreground">{t}</span>
            <input type="color" aria-label={t} value={hex(value(t))} onChange={(e) => edit(t, e.target.value)} />
            <Input value={value(t)} onChange={(e) => edit(t, e.target.value)} />
          </label>
        ))}
        {sharedTokens.map((t) => (
          <label key={t} className="grid grid-cols-[minmax(0,110px)_minmax(0,1fr)] items-center gap-2">
            <span className="text-muted-foreground">{t === "sans" || t === "mono" ? `font (${t})` : t}</span>
            <Input value={value(t)} onChange={(e) => edit(t, e.target.value)} />
          </label>
        ))}
      </section>

      <section className="grid gap-2">
        <div className="flex gap-2">
          <Input className="min-w-0" placeholder="theme name, e.g. my-dusk" value={name} onChange={(e) => setName(e.target.value)} />
          <Button size="sm" disabled={!name} onClick={save}>
            Save
          </Button>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="outline" asChild>
            <label>
              Import JSON
              <input type="file" accept="application/json,.json" className="hidden" onChange={importFile} />
            </label>
          </Button>
          <Button size="sm" variant="outline" onClick={() => navigator.clipboard.writeText(JSON.stringify(theme, null, 2))}>
            Copy JSON
          </Button>
          {draft && (
            <Button size="sm" variant="ghost" onClick={() => setDraft(null)}>
              Discard changes
            </Button>
          )}
        </div>
        <p className="text-muted-foreground">
          Saved themes live in <code className="font-mono">{String(node.fields?.dir ?? "")}</code>.
        </p>
        {error && <p className="text-bad">{error}</p>}
      </section>
    </div>
  );
}
