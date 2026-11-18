import type { WorkflowDefinition } from "../api/workflows";
import type { Positions } from "./graphModel";
export interface FixtureDraft {
  definition: WorkflowDefinition;
  positions: Positions;
  savedAt: string;
}
const database = "elenio-phase1-fixtures";
function open(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(database, 1);
    req.onupgradeneeded = () => req.result.createObjectStore("drafts");
    req.onsuccess = () => resolve(req.result);
    req.onerror = () =>
      reject(
        new Error(
          "Local draft storage is unavailable. Export JSON to keep your work.",
        ),
      );
  });
}
export async function readDraft(): Promise<FixtureDraft | undefined> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const tx = db.transaction("drafts", "readonly"),
      req = tx.objectStore("drafts").get("vendor-payment");
    req.onsuccess = () => resolve(req.result as FixtureDraft | undefined);
    req.onerror = () =>
      reject(new Error("Could not read the local fixture draft."));
    tx.oncomplete = () => db.close();
  });
}
export async function writeDraft(value: FixtureDraft): Promise<void> {
  const db = await open();
  return new Promise((resolve, reject) => {
    const tx = db.transaction("drafts", "readwrite");
    tx.objectStore("drafts").put(value, "vendor-payment");
    tx.oncomplete = () => {
      db.close();
      resolve();
    };
    tx.onerror = () => {
      db.close();
      reject(
        new Error("Could not save locally. Export JSON to keep your work."),
      );
    };
  });
}
