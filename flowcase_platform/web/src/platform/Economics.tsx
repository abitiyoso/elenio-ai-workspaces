import { useEffect, useState } from "react";
import {
  Badge,
  Button,
  DialogPanel,
  Notice,
} from "../../../../packages/ui/primitives";
import {
  estimate,
  sample,
  validateScenario,
  type Scenario,
} from "./economicsModel";
import "./economics.css";
type Snapshot = {
  schema: 1;
  revision: number;
  createdAt: string;
  source: string;
  effectiveDate: string;
  scenario: Scenario;
};
const storeKey = "elenio-local-economics-v1";
function validDate(value: unknown): value is string {
  return (
    typeof value === "string" &&
    /^\d{4}-\d{2}-\d{2}$/.test(value) &&
    Number.isFinite(Date.parse(value)) &&
    new Date(value).toISOString().slice(0, 10) === value
  );
}
const currency = (n: number | null) =>
  n === null
    ? "N/A"
    : new Intl.NumberFormat("id-ID", {
        style: "currency",
        currency: "IDR",
        maximumFractionDigits: 0,
      }).format(n);
const decimals = (n: number | null, suffix = "") =>
  n === null ? "N/A" : `${n.toFixed(2)}${suffix}`;
function snapshots(add?: Snapshot): Promise<Snapshot[]> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(storeKey, 1);
    req.onupgradeneeded = () =>
      req.result.createObjectStore("revisions", {
        keyPath: "revision",
        autoIncrement: true,
      });
    req.onerror = () =>
      reject(
        new Error(
          "Scenario storage unavailable. Export your draft to retain it.",
        ),
      );
    req.onsuccess = () => {
      const db = req.result;
      const tx = db.transaction("revisions", add ? "readwrite" : "readonly");
      const store = tx.objectStore("revisions");
      let items: Snapshot[] = [];
      const get = store.getAll();
      get.onsuccess = () => {
        try {
          items = get.result as Snapshot[];
          for (const item of items) {
            if (
              item.schema !== 1 ||
              !item.source ||
              !validDate(item.effectiveDate)
            )
              throw new Error("Invalid saved scenario");
            validateScenario(item.scenario);
          }
          if (add) {
            if (items.length >= 20)
              throw new Error(
                "Twenty browser revisions reached. Export your draft.",
              );
            const { revision: unused, ...record } = add;
            void unused;
            const put = store.add(record);
            put.onsuccess = () => {
              items.push({ ...record, revision: Number(put.result) });
            };
          }
        } catch (e) {
          reject(e);
          tx.abort();
        }
      };
      tx.oncomplete = () => {
        db.close();
        resolve(items);
      };
      tx.onerror = tx.onabort = () => {
        db.close();
        reject(
          new Error("Scenario storage failed; current inputs are retained."),
        );
      };
    };
  });
}
export function Economics() {
  const [scenario, setScenario] = useState<Scenario>({ ...sample }),
    [source, setSource] = useState("Development kit — synthetic fixture"),
    [effectiveDate, setDate] = useState("2026-11-19"),
    [history, setHistory] = useState<{ items: Snapshot[]; error: string }>({
      items: [],
      error: "",
    }),
    [loadingHistory, setLoadingHistory] = useState(true),
    [saving, setSaving] = useState(false),
    [notice, setNotice] = useState(""),
    [error, setError] = useState(""),
    [importOpen, setImportOpen] = useState(false),
    [json, setJson] = useState("");
  useEffect(() => {
    let cancelled = false;
    snapshots().then(
      (items) => {
        if (!cancelled) {
          setHistory({ items, error: "" });
          setLoadingHistory(false);
        }
      },
      () => {
        if (!cancelled) {
          setHistory({
            items: [],
            error:
              "Saved scenarios could not be read. Export the current draft to retain it.",
          });
          setLoadingHistory(false);
        }
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);
  let result: ReturnType<typeof estimate> | null = null,
    validation = "";
  try {
    result = estimate(scenario);
  } catch (e) {
    validation = e instanceof Error ? e.message : "Invalid inputs";
  }
  const document = (revision: number): Snapshot => ({
    schema: 1,
    revision,
    createdAt: new Date().toISOString(),
    source,
    effectiveDate,
    scenario: { ...scenario },
  });
  function exportFile() {
    const blob = new Blob(
      [JSON.stringify(document(history.items.length + 1), null, 2)],
      { type: "application/json" },
    );
    const url = URL.createObjectURL(blob);
    const a = window.document.createElement("a");
    a.href = url;
    a.download = "elenio-economics-scenario.json";
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
  }
  function number(
    key: keyof Scenario,
    label: string,
    options: { prob?: boolean; attempt?: boolean; rate?: boolean } = {},
  ) {
    const value = scenario[key];
    const factor = options.prob ? 100 : options.attempt ? 10000 : 1;
    return (
      <label className="e-field" key={key}>
        {label}
        <input
          type="number"
          min={options.attempt ? 1 : 0}
          max={options.prob ? 100 : options.attempt ? 10 : undefined}
          step={options.prob ? 0.01 : options.attempt ? 0.01 : 1}
          value={
            value === null || !Number.isFinite(value) ? "" : value / factor
          }
          onChange={(e) => {
            setScenario({
              ...scenario,
              [key]:
                e.target.value === ""
                  ? options.rate
                    ? null
                    : NaN
                  : factor === 1
                    ? Number(e.target.value)
                    : Math.round(Number(e.target.value) * factor),
            });
            setNotice("");
          }}
        />
        {options.rate && (
          <small>Blank = unpriced; 0 = explicitly no incremental charge.</small>
        )}
      </label>
    );
  }
  const valid = !validation && !!source.trim() && validDate(effectiveDate);
  return (
    <section className="page economics-page">
      <header className="page-heading">
        <div>
          <span className="eyebrow">AI ECONOMICS</span>
          <h1>Plan the cost. Check the value.</h1>
          <p>
            Compare a vendor workflow scenario with its conventional process.
          </p>
        </div>
        <Badge tone="amber">Estimated scenario · IDR</Badge>
      </header>
      <Notice>
        Planning template with synthetic starting values. No provider prices,
        measured sandbox usage or production costs are connected. Savings are
        scenario differences, not guaranteed cash savings.
      </Notice>
      <div className="economics-actions">
        <Button disabled={!valid} onClick={exportFile}>
          Export scenario JSON
        </Button>
        <Button
          onClick={() => {
            setImportOpen(true);
            setJson("");
            setError("");
          }}
        >
          Import scenario
        </Button>
        <Button
          disabled={!valid || !!history.error || loadingHistory || saving}
          onClick={async () => {
            setSaving(true);
            setError("");
            try {
              const items = await snapshots(document(history.items.length + 1));
              setHistory({ items, error: "" });
              setNotice(
                "Revision " +
                  items.at(-1)?.revision +
                  " saved on this browser.",
              );
            } catch (e) {
              setError(e instanceof Error ? e.message : "Browser save failed");
            } finally {
              setSaving(false);
            }
          }}
        >
          Save browser revision
        </Button>
        <Button
          onClick={() => {
            setScenario({ ...sample });
            setSource("Development kit — synthetic fixture");
            setNotice(
              "Synthetic starting assumptions restored; saved revisions retained.",
            );
          }}
        >
          Load synthetic sample
        </Button>
      </div>
      {notice && <Notice>{notice}</Notice>}
      {(error || history.error || validation) && (
        <Notice error>{error || history.error || validation}</Notice>
      )}
      <div className="economics-grid">
        <div className="economics-inputs">
          <section className="surface economics-card">
            <h2>Scenario assumptions</h2>
            <p>
              Same attempted workload for both options. Completed acceptable
              volume is an explicit quality assumption.
            </p>
            <div className="economics-fields">
              {number("volume", "Monthly attempted cases")}
              {number("completionBps", "Acceptable completion (%)", {
                prob: true,
              })}
              {number("unmatchedBps", "Unmatched branch (%)", { prob: true })}
              {number("approvedBps", "Cases reaching posting (%)", {
                prob: true,
              })}
              {number("extractionAttemptsBps", "Expected extraction attempts", {
                attempt: true,
              })}
              <label className="e-field">
                Rate assumptions source
                <input
                  value={source}
                  onChange={(e) => setSource(e.target.value)}
                />
              </label>
              <label className="e-field">
                Assumptions effective date
                <input
                  type="date"
                  value={effectiveDate}
                  onChange={(e) => setDate(e.target.value)}
                />
              </label>
            </div>
          </section>
          <section className="surface economics-card">
            <h2>Machine cost per visit</h2>
            <p>
              Whole IDR per visit. Extraction includes the entered expected
              attempts. Exception and posting use their explicit reach
              probabilities.
            </p>
            <div className="economics-fields">
              {number("extraction", "Extraction", { rate: true })}
              {number("lookup", "ERP lookup", { rate: true })}
              {number("exception", "Exception agent", { rate: true })}
              {number("policy", "Policy step", { rate: true })}
              {number("posting", "ERP posting", { rate: true })}
            </div>
            <Notice>
              This planning model does not infer graph reach or correlated
              conditions. It assumes extraction, lookup and policy are always
              reached; trigger and approval have zero machine charge. Review
              labor applies to every attempt. Parallel execution, arbitrary
              workflow graphs and retry distributions need their own cost model.
            </Notice>
          </section>
          <section className="surface economics-card">
            <h2>People, overhead & baseline</h2>
            <div className="economics-fields">
              {number("human", "Human review per attempted case (IDR)")}
              {number("fixed", "AI fixed monthly overhead (IDR)")}
              {number("implementation", "One-time implementation (IDR)")}
              {number("amortization", "Amortization months")}
              {number(
                "conventional",
                "Conventional variable cost per case (IDR)",
              )}
              {number(
                "conventionalFixed",
                "Conventional fixed monthly cost (IDR)",
              )}
              {number("horizon", "TCO horizon months")}
            </div>
          </section>
        </div>
        <aside className="economics-results">
          <section className="surface economics-card">
            <span className="eyebrow">SCENARIO RESULTS</span>
            <h2>Cost per acceptable case</h2>
            {result?.priced ? (
              <>
                <strong
                  className="economics-total"
                  data-testid="cost-completed"
                >
                  {currency(result.costPerCompleted)}
                </strong>
                <p>
                  {(scenario.volume * scenario.completionBps) / 10000} expected
                  acceptable cases / month. Failed attempts remain in cost.
                </p>
                <dl>
                  {[
                    ["Expected machine / attempt", result.machine],
                    ["Variable cost / attempt", result.variable],
                    ["AI operating / month", result.operating],
                    ["AI fully allocated / month", result.allocated],
                    ["Conventional / month", result.baseline],
                    [
                      "Operating difference / month",
                      result.operatingDifference,
                    ],
                    ["Full-cost difference / month", result.fullDifference],
                    ["AI TCO", result.aiTco],
                    ["Conventional TCO", result.conventionalTco],
                  ].map(([label, value]) => (
                    <div key={label}>
                      <dt>{label}</dt>
                      <dd>{currency(value as number)}</dd>
                    </div>
                  ))}
                </dl>
                <div className="economics-facts">
                  <p>
                    Full-cost reduction{" "}
                    <strong>
                      {decimals(
                        result.reduction === null
                          ? null
                          : result.reduction * 100,
                        "%",
                      )}
                    </strong>
                  </p>
                  <p>
                    Break-even{" "}
                    <strong>
                      {result.breakEven === null
                        ? "N/A"
                        : `${result.breakEven} cases/mo`}
                    </strong>
                  </p>
                  <p>{result.breakEvenNote}</p>
                  <p>
                    Simple payback{" "}
                    <strong>{decimals(result.payback, " months")}</strong>
                  </p>
                  <p>
                    Cost-based ROI{" "}
                    <strong>
                      {decimals(
                        result.roi === null ? null : result.roi * 100,
                        "%",
                      )}
                    </strong>
                  </p>
                </div>
                <details>
                  <summary>Formula and cost drivers</summary>
                  <p>
                    ROI = (conventional TCO − AI TCO) / AI TCO. TCO includes
                    implementation once; amortization is used only in monthly
                    full cost. Displayed IDR is rounded.
                  </p>
                  <ul>
                    {result.rows.map((r) => (
                      <li key={r.label}>
                        {r.label}: {currency(r.expected)} expected / attempt ·
                        reach {r.reach / 100}% · {r.attempts / 10000} expected
                        attempts
                      </li>
                    ))}
                  </ul>
                </details>
              </>
            ) : result ? (
              <Notice error>
                Unpriced: {result.missing.join(", ")}. Totals and ROI are
                unavailable until reached rates are supplied.
              </Notice>
            ) : (
              <Notice error>Correct the scenario inputs to calculate.</Notice>
            )}
          </section>
          <section className="surface economics-card">
            <h2>Browser revisions</h2>
            <p>
              Optional local copies on this device. These are not approved rate
              cards or tenant-shared records. Export JSON to retain or share an
              explicit revision.
            </p>
            {history.items.length ? (
              <ol>
                {history.items.map((s) => (
                  <li key={s.revision}>
                    <Button
                      onClick={() => {
                        setScenario({ ...s.scenario });
                        setSource(s.source);
                        setDate(s.effectiveDate);
                        setNotice(
                          `Loaded browser revision ${s.revision}. Further saves create a new revision.`,
                        );
                      }}
                    >
                      Load revision {s.revision}
                    </Button>
                    <small>{new Date(s.createdAt).toLocaleString()}</small>
                  </li>
                ))}
              </ol>
            ) : (
              <p>No saved revisions.</p>
            )}
          </section>
        </aside>
      </div>
      {importOpen && (
        <DialogPanel
          title="Import an estimated scenario"
          description="Review the JSON before replacing the current planning inputs. Saved browser revisions are retained."
          onClose={() => setImportOpen(false)}
        >
          <label className="e-field">
            Scenario JSON
            <textarea
              rows={10}
              value={json}
              onChange={(e) => setJson(e.target.value)}
            />
          </label>
          {error && <Notice error>{error}</Notice>}
          <Button
            onClick={() => {
              try {
                const item = JSON.parse(json) as Snapshot;
                if (
                  item.schema !== 1 ||
                  typeof item.source !== "string" ||
                  !item.source.trim() ||
                  !validDate(item.effectiveDate)
                )
                  throw new Error("Unsupported scenario document.");
                validateScenario(item.scenario);
                setScenario({ ...item.scenario });
                setSource(item.source);
                setDate(item.effectiveDate);
                setImportOpen(false);
                setError("");
                setNotice("Imported planning inputs. No server data changed.");
              } catch (e) {
                setError(e instanceof Error ? e.message : "Invalid scenario");
              }
            }}
          >
            Replace planning inputs
          </Button>
        </DialogPanel>
      )}
    </section>
  );
}
