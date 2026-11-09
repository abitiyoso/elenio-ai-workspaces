import type { DataProperty, DataSchema } from "../api/cases";
import type { FormValues } from "./formData";

const label = (name: string, p: DataProperty) => p.title ?? name.replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase());

/**
 * Renders inputs for the safe schema subset (scalars, string arrays, enums); properties marked
 * "x-hidden" are accepted by the server but never shown. The server validates the submitted values
 * again; this form only shapes them.
 */
export function DataForm({ schema, values, onChange, idPrefix }: { schema: DataSchema; values: FormValues; onChange: (v: FormValues) => void; idPrefix: string }) {
  const props = Object.entries(schema.properties ?? {}).filter(([, p]) => p.type !== "object" && !p["x-hidden"]);
  const required = new Set(schema.required ?? []);
  if (props.length === 0) return null;
  return (
    <>
      {props.map(([name, p]) => {
        const id = `${idPrefix}-${name}`;
        const set = (v: string | boolean) => onChange({ ...values, [name]: v });
        const text = label(name, p) + (required.has(name) ? " *" : "");
        if (p.type === "boolean") {
          return (
            <label key={name} className="check form-check" htmlFor={id}>
              <input id={id} type="checkbox" checked={values[name] === true} onChange={(e) => set(e.target.checked)} /> {text}
            </label>
          );
        }
        return (
          <div key={name}>
            <label className="label" htmlFor={id}>{text}</label>
            {p.enum ? (
              <select className="field" id={id} value={String(values[name] ?? "")} onChange={(e) => set(e.target.value)}>
                <option value="">—</option>
                {p.enum.map((v) => <option key={String(v)} value={String(v)}>{String(v)}</option>)}
              </select>
            ) : (
              <input className="field" id={id} value={String(values[name] ?? "")} onChange={(e) => set(e.target.value)}
                type={p.type === "number" || p.type === "integer" ? "number" : p.format === "date-time" ? "datetime-local" : p.format === "date" ? "date" : "text"}
                placeholder={p.type === "array" ? "Comma-separated values" : p.description ?? ""} />
            )}
          </div>
        );
      })}
    </>
  );
}
