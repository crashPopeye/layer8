/** One input of an action form, derived from a property of the action's args schema. */
export interface Field {
  name: string;
  kind: "text" | "number" | "boolean" | "enum" | "json";
  required: boolean;
  description?: string;
  options?: unknown[];
}

interface Schema {
  type?: string;
  description?: string;
  enum?: unknown[];
  required?: string[];
  properties?: Record<string, Schema>;
}

const kindOf = (s: Schema): Field["kind"] =>
  s.enum
    ? "enum"
    : s.type === "string" || !s.type
      ? "text"
      : s.type === "number" || s.type === "integer"
        ? "number"
        : s.type === "boolean"
          ? "boolean"
          : "json";

/** Lists the form fields of an object schema: required ones first, in schema order. */
export function fieldsOf(schema: unknown): Field[] {
  const s = (schema ?? {}) as Schema;
  const required = s.required ?? [];
  return Object.entries(s.properties ?? {})
    .map(([name, p]) => ({
      name,
      kind: kindOf(p),
      required: required.includes(name),
      description: p.description,
      options: p.enum,
    }))
    .sort((a, b) => Number(b.required) - Number(a.required));
}

/** Form values by field name: strings from inputs, booleans from checkboxes. */
export type Values = Record<string, string | boolean | undefined>;

/** Turns form values into action args, dropping empty optional fields, or returns the first problem. */
export function argsFrom(fields: Field[], values: Values): { args: Record<string, unknown> } | { error: string } {
  const args: Record<string, unknown> = {};
  for (const f of fields) {
    const v = values[f.name];
    if (f.kind === "boolean") {
      if (v !== undefined) args[f.name] = v === true;
      continue;
    }
    const text = typeof v === "string" ? v.trim() : "";
    if (text === "") {
      if (f.required) return { error: `${f.name} is required` };
      continue;
    }
    if (f.kind === "number") {
      if (Number.isNaN(Number(text))) return { error: `${f.name} must be a number` };
      args[f.name] = Number(text);
    } else if (f.kind === "json") {
      try {
        args[f.name] = JSON.parse(text);
      } catch {
        return { error: `${f.name} must be JSON` };
      }
    } else if (f.kind === "enum") {
      args[f.name] = f.options!.find((o) => String(o) === text);
    } else {
      args[f.name] = v;
    }
  }
  return { args };
}
