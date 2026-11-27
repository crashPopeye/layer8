import { ChevronDown } from "lucide-react";
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover";
import { cn } from "@/lib/utils";

/** What a session runs with; empty strings mean the harness default. */
export interface Settings {
  harness: string;
  model: string;
  effort: string;
  permissionMode: string;
}

/** A model the harness offers, with the effort levels it accepts. */
export interface Model {
  value: string;
  displayName: string;
  efforts?: string[];
}

type Option = [value: string, label: string];

export const permissionModes: Option[] = [
  ["", "default"],
  ["acceptEdits", "accept edits"],
  ["auto", "auto"],
  ["plan", "plan"],
  ["bypassPermissions", "bypass"],
];
const defaultEfforts = ["low", "medium", "high", "xhigh", "max"];

/** The model a value names without its variant suffix: `opus` for `opus[1m]`. */
export const family = (value: string) => value.replace(/\[.*\]$/, "");

/** The variant a value names, `1M` for `opus[1m]`, empty for the standard one. */
const variant = (value: string) => /\[(.*)\]$/.exec(value)?.[1].toUpperCase() ?? "";

/** The model families in offer order, each labelled by its first model and picking it. */
export function families(models: Model[]): Option[] {
  const seen = new Map<string, Option>();
  for (const m of models) if (!seen.has(family(m.value))) seen.set(family(m.value), [m.value, m.displayName]);
  return [...seen.values()];
}

/** The variants of the family value belongs to, when it has more than one. */
export function versions(models: Model[], value: string): Option[] {
  const same = models.filter((m) => family(m.value) === family(value));
  return same.length > 1 ? same.map((m) => [m.value, variant(m.value) || "standard"]) : [];
}

/** The one-line summary the pill shows: `Opus · high · plan`, default parts left out. */
export function settingsLabel(s: Settings, models: Model[]): string {
  const model = models.find((m) => m.value === s.model)?.displayName ?? (s.model || "Default model");
  const mode = s.permissionMode && permissionModes.find(([v]) => v === s.permissionMode)?.[1];
  return [model, s.effort, mode || s.permissionMode].filter(Boolean).join(" · ");
}

function Segmented({ label, value, options, onPick }: { label: string; value: string; options: Option[]; onPick: (v: string) => void }) {
  return (
    <div className="space-y-1.5">
      <p className="text-[11px] font-medium text-muted-foreground">{label}</p>
      <div role="radiogroup" aria-label={label} className="flex flex-wrap gap-0.5 rounded-md bg-muted p-0.5">
        {options.map(([v, l]) => (
          <button
            key={v || "default"}
            type="button"
            role="radio"
            aria-checked={v === value}
            onClick={() => onPick(v)}
            className={cn(
              "flex-1 rounded-[5px] px-2 py-1 text-[12px] whitespace-nowrap text-muted-foreground transition-colors outline-none hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50",
              v === value && "bg-background font-medium text-foreground shadow-xs",
            )}
          >
            {l}
          </button>
        ))}
      </div>
    </div>
  );
}

/** One pill summarising harness, model, effort and permission mode, opening a popover to change them; harnesses is empty when the harness is fixed. */
export function ModelControl({
  settings,
  models,
  harnesses,
  onChange,
}: {
  settings: Settings;
  models: Model[];
  harnesses: string[];
  onChange: (patch: Partial<Settings>) => void;
}) {
  const model = models.find((m) => m.value === settings.model);
  const efforts = model?.efforts ?? defaultEfforts;
  const options = families(models);
  const current = settings.model && (options.find(([v]) => family(v) === family(settings.model))?.[0] ?? settings.model);
  const pickFamily = (v: string) => {
    const suffix = /\[.*\]$/.exec(settings.model)?.[0];
    const same = suffix && models.find((m) => m.value === family(v) + suffix);
    onChange({ model: same ? same.value : v });
  };
  return (
    <Popover>
      <PopoverTrigger asChild>
        <button
          type="button"
          title="Model settings"
          className="flex h-7 max-w-56 items-center gap-1 rounded-md px-2 text-[12px] text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50 data-[state=open]:bg-accent data-[state=open]:text-foreground"
        >
          <span className="truncate">{settingsLabel(settings, models)}</span>
          <ChevronDown className="size-3 shrink-0 opacity-60" />
        </button>
      </PopoverTrigger>
      <PopoverContent align="start" side="top" className="w-80 space-y-3">
        {harnesses.length > 1 && (
          <Segmented label="Harness" value={settings.harness} options={harnesses.map((h) => [h, h])} onPick={(v) => onChange({ harness: v, model: "", effort: "" })} />
        )}
        <Segmented label="Model" value={current} options={[["", "default"], ...options]} onPick={pickFamily} />
        {versions(models, settings.model).length > 0 && (
          <Segmented label="Version" value={settings.model} options={versions(models, settings.model)} onPick={(v) => onChange({ model: v })} />
        )}
        {efforts.length > 0 && (
          <Segmented label="Effort" value={settings.effort} options={[["", "default"], ...efforts.map((e): Option => [e, e])]} onPick={(v) => onChange({ effort: v })} />
        )}
        <Segmented label="Permissions" value={settings.permissionMode} options={permissionModes} onPick={(v) => onChange({ permissionMode: v })} />
      </PopoverContent>
    </Popover>
  );
}
