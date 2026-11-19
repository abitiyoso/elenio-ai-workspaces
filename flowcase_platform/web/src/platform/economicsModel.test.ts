import { describe, it, expect } from "vitest";
import { estimate, sample } from "./economicsModel";
const priced = (change: Partial<typeof sample> = {}) => {
  const r = estimate({ ...sample, ...change });
  if (!r.priced) throw new Error("unpriced");
  return r;
};
describe("kit Economics fixture, fixed-point IDR", () => {
  it("matches the full approved synthetic acceptance arithmetic", () => {
    const r = priced();
    expect(r.machine).toBe(1050);
    expect(r.variable).toBe(2550);
    expect(r.operating).toBe(4550000);
    expect(r.allocated).toBe(5550000);
    expect(r.costPerAttempt).toBe(5550);
    expect(r.costPerCompleted).toBe(5550);
    expect(r.baseline).toBe(12000000);
    expect(r.operatingDifference).toBe(7450000);
    expect(r.fullDifference).toBe(6450000);
    expect(r.reduction).toBe(0.5375);
    expect(r.breakEven).toBe(318);
    expect(r.payback).toBeCloseTo(1.6107, 4);
    expect(r.aiTco).toBe(66600000);
    expect(r.conventionalTco).toBe(144000000);
    expect(r.roi).toBeCloseTo(1.162162, 6);
  });
  it("weights conditional reach, explicit retries and approval-dependent posting", () => {
    expect(priced({ unmatchedBps: 0 }).machine).toBe(900);
    expect(priced({ unmatchedBps: 10000 }).machine).toBe(1500);
    expect(priced({ approvedBps: 0 }).machine).toBe(750);
    expect(priced({ extractionAttemptsBps: 20000 }).machine).toBe(1300);
  });
  it("keeps costs of failed attempts in numerator and returns N/A for zero denominator", () => {
    const r = priced({ completionBps: 5000 });
    expect(r.allocated).toBe(5550000);
    expect(r.costPerCompleted).toBe(11100);
    expect(priced({ completionBps: 0 }).costPerCompleted).toBeNull();
    expect(priced({ volume: 0 }).costPerAttempt).toBeNull();
    expect(priced({ volume: 0 }).allocated).toBe(3000000);
  });
  it("never treats unknown reached rates as free, permits explicit zero and unreachable unpriced nodes", () => {
    expect(estimate({ ...sample, extraction: null })).toEqual({
      priced: false,
      missing: ["Extraction"],
    });
    expect(priced({ extraction: 0 }).machine).toBe(800);
    expect(priced({ exception: null, unmatchedBps: 0 }).machine).toBe(900);
  });
  it("handles negative benefit and low-volume fixed advantage without false break-even claims", () => {
    const r = priced({ conventional: 0 });
    expect(r.fullDifference).toBeLessThan(0);
    expect(r.payback).toBeNull();
    expect(r.breakEven).toBeNull();
    expect(
      priced({ conventional: 0, conventionalFixed: 10000000 }).breakEvenNote,
    ).toContain("low-volume");
    expect(
      priced({ conventional: 0, conventionalFixed: 0 }).reduction,
    ).toBeNull();
  });
  it("rejects invalid probabilities, unsafe integers, negative costs, zero horizon and unbounded retries", () => {
    for (const change of [
      { volume: -1 },
      { volume: NaN },
      { fixed: Infinity },
      { human: 0.5 },
      { unmatchedBps: 10001 },
      { extractionAttemptsBps: 100001 },
      { amortization: 0 },
      { horizon: 0 },
    ])
      expect(() => priced(change)).toThrow();
  });
});
