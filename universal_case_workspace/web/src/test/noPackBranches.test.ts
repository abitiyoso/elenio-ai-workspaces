import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

// The shared UI must stay generic: no pack keys or pack-specific branches outside tests and fixtures.
function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) return name === "test" || name === "fixtures" ? [] : sources(p);
    return /\.(tsx?|css)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [p] : [];
  });
}

describe("generic renderer guard", () => {
  const root = join(process.cwd(), "src");
  const files = sources(root);

  it("scans the application sources", () => {
    expect(files.length).toBeGreaterThan(20);
  });

  it("never names a domain pack in application code", () => {
    const offenders = files.filter((f) => /sim[.\-_]tender|legal\.contract-review|lab\.poc-assessment/i.test(readFileSync(f, "utf8"))).map((f) => relative(root, f));
    expect(offenders).toEqual([]);
  });

  it("never branches on a pack key", () => {
    const offenders = files.filter((f) => /pack_key\s*(===|!==|==|!=)\s*["'`]|case\s+["'`][a-z]+\.[a-z-]+["'`]\s*:/.test(readFileSync(f, "utf8"))).map((f) => relative(root, f));
    expect(offenders).toEqual([]);
  });
});
