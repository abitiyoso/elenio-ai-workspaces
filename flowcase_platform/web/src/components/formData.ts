import type { DataSchema } from "../api/cases";

export type FormValues = Record<string, string | boolean>;

/** Converts form strings to the JSON types the schema declares; empty fields are omitted. */
export function toData(schema: DataSchema, values: FormValues): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [name, p] of Object.entries(schema.properties ?? {})) {
    const v = values[name];
    if (v === undefined || v === "") continue;
    if (p.type === "boolean") out[name] = v === true;
    else if (p.type === "number" || p.type === "integer") out[name] = Number(v);
    else if (p.type === "array") out[name] = String(v).split(",").map((s) => s.trim()).filter(Boolean);
    else if (p.format === "date-time") out[name] = new Date(String(v)).toISOString();
    else out[name] = v;
  }
  return out;
}
