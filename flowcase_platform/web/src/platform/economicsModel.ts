export interface Scenario {
  volume: number;
  completionBps: number;
  unmatchedBps: number;
  approvedBps: number;
  extractionAttemptsBps: number;
  extraction: number | null;
  lookup: number | null;
  exception: number | null;
  policy: number | null;
  posting: number | null;
  human: number;
  fixed: number;
  implementation: number;
  amortization: number;
  conventional: number;
  conventionalFixed: number;
  horizon: number;
}
export const sample: Scenario = {
  volume: 1000,
  completionBps: 10000,
  unmatchedBps: 2500,
  approvedBps: 10000,
  extractionAttemptsBps: 10000,
  extraction: 250,
  lookup: 200,
  exception: 600,
  policy: 150,
  posting: 300,
  human: 1500,
  fixed: 2000000,
  implementation: 12000000,
  amortization: 12,
  conventional: 12000,
  conventionalFixed: 0,
  horizon: 12,
};
const scale = 100000000n;
const money = (n: bigint) => {
  if (
    n > BigInt(Number.MAX_SAFE_INTEGER) * scale ||
    n < -BigInt(Number.MAX_SAFE_INTEGER) * scale
  )
    throw new Error("Scenario totals exceed the supported display range.");
  return Number(n) / Number(scale);
};
export function validateScenario(s: Scenario) {
  if (!s || typeof s !== "object" || Object.keys(s).some((k) => !(k in sample)))
    throw new Error("Unsupported scenario fields.");
  for (const key of Object.keys(sample) as (keyof Scenario)[]) {
    const value = s[key];
    if (
      value === null &&
      ["extraction", "lookup", "exception", "policy", "posting"].includes(key)
    )
      continue;
    if (
      typeof value !== "number" ||
      !Number.isSafeInteger(value) ||
      value < 0 ||
      value > 1000000000000
    )
      throw new Error(
        `${key}: enter a nonnegative whole number within the supported range.`,
      );
  }
  for (const key of ["completionBps", "unmatchedBps", "approvedBps"] as const)
    if (s[key] > 10000)
      throw new Error(`${key}: probability must be between 0 and 100%.`);
  if (s.extractionAttemptsBps < 10000 || s.extractionAttemptsBps > 100000)
    throw new Error("Expected extraction attempts must be between 1 and 10.");
  if (
    s.amortization < 1 ||
    s.amortization > 600 ||
    s.horizon < 1 ||
    s.horizon > 600
  )
    throw new Error("Amortization and TCO horizon must be 1–600 months.");
}
export function estimate(s: Scenario) {
  validateScenario(s);
  const costs = [
    {
      label: "Extraction",
      rate: s.extraction,
      reach: 10000,
      attempts: s.extractionAttemptsBps,
    },
    { label: "ERP lookup", rate: s.lookup, reach: 10000, attempts: 10000 },
    {
      label: "Exception agent",
      rate: s.exception,
      reach: s.unmatchedBps,
      attempts: 10000,
    },
    { label: "Policy step", rate: s.policy, reach: 10000, attempts: 10000 },
    {
      label: "ERP posting",
      rate: s.posting,
      reach: s.approvedBps,
      attempts: 10000,
    },
  ];
  const missing = costs
    .filter((c) => c.rate === null && c.reach > 0)
    .map((c) => c.label);
  if (missing.length) return { priced: false as const, missing };
  const rows = costs.map((c) => ({
    ...c,
    expected: BigInt(c.rate ?? 0) * BigInt(c.reach) * BigInt(c.attempts),
  }));
  const machine = rows.reduce((a, c) => a + c.expected, 0n),
    variable = machine + BigInt(s.human) * scale;
  const operating = BigInt(s.volume) * variable + BigInt(s.fixed) * scale;
  const allocated =
    operating + (BigInt(s.implementation) * scale) / BigInt(s.amortization);
  const baseline =
    (BigInt(s.volume) * BigInt(s.conventional) + BigInt(s.conventionalFixed)) *
    scale;
  const operatingDifference = baseline - operating,
    fullDifference = baseline - allocated;
  const aiTco =
    BigInt(s.implementation) * scale + BigInt(s.horizon) * operating;
  const conventionalTco = BigInt(s.horizon) * baseline;
  const unitAdvantage = BigInt(s.conventional) * scale - variable;
  const fixedDifference =
    BigInt(s.fixed - s.conventionalFixed) * scale +
    (BigInt(s.implementation) * scale) / BigInt(s.amortization);
  const breakEven =
    unitAdvantage > 0n
      ? Number(
          fixedDifference <= 0n
            ? 0n
            : (fixedDifference + unitAdvantage - 1n) / unitAdvantage,
        )
      : null;
  return {
    priced: true as const,
    missing: [],
    rows: rows.map((c) => ({ ...c, expected: money(c.expected) })),
    machine: money(machine),
    variable: money(variable),
    operating: money(operating),
    allocated: money(allocated),
    baseline: money(baseline),
    operatingDifference: money(operatingDifference),
    fullDifference: money(fullDifference),
    costPerAttempt: s.volume ? money(allocated) / s.volume : null,
    costPerCompleted:
      s.volume && s.completionBps
        ? money(allocated) / ((s.volume * s.completionBps) / 10000)
        : null,
    reduction: baseline ? Number(fullDifference) / Number(baseline) : null,
    payback:
      operatingDifference > 0n
        ? Number(BigInt(s.implementation) * scale) / Number(operatingDifference)
        : null,
    breakEven,
    breakEvenNote:
      unitAdvantage <= 0n
        ? fixedDifference < 0n
          ? "AI fixed costs are lower but variable costs are not. Compare low-volume scenarios; no minimum-volume break-even is asserted."
          : "No minimum-volume break-even: AI has no positive variable-cost advantage."
        : "Minimum monthly attempted volume, rounded up.",
    aiTco: money(aiTco),
    conventionalTco: money(conventionalTco),
    roi: aiTco ? Number(conventionalTco - aiTco) / Number(aiTco) : null,
  };
}
