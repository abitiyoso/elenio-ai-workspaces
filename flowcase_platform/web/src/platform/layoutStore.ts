import type { Positions } from "./graphModel";
export interface LayoutScope {
  tenant: string;
  user: string;
  versionID: string;
}
const keyFor = (scope: LayoutScope) =>
  JSON.stringify([scope.tenant, scope.user, scope.versionID]);
function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open("elenio-visual-layout-v1", 1);
    req.onupgradeneeded = () => req.result.createObjectStore("layouts");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () =>
      reject(new Error("Device layout storage is unavailable."));
  });
}
/** Local visual metadata only. Never store service documents, credentials, roles or execution state. */
export async function readLayout(scope: LayoutScope): Promise<Positions> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const tx = db.transaction("layouts", "readonly"),
      req = tx.objectStore("layouts").get(keyFor(scope));
    req.onsuccess = () => {
      const raw: unknown = req.result;
      if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
        resolve({});
        return;
      }
      resolve(
        Object.fromEntries(
          Object.entries(raw)
            .filter((entry): entry is [string, { x: number; y: number }] => {
              const p = entry[1] as { x?: unknown; y?: unknown } | null;
              return (
                !!p &&
                typeof p === "object" &&
                typeof p.x === "number" &&
                Number.isFinite(p.x) &&
                typeof p.y === "number" &&
                Number.isFinite(p.y)
              );
            })
            .slice(0, 200),
        ),
      );
    };
    req.onerror = () => reject(new Error("Could not read visual positions."));
    tx.oncomplete = () => db.close();
  });
}
export async function writeLayout(
  scope: LayoutScope,
  positions: Positions,
): Promise<void> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const tx = db.transaction("layouts", "readwrite");
    tx.objectStore("layouts").put(positions, keyFor(scope));
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(new Error("Could not save visual positions."));
    };
  });
}
