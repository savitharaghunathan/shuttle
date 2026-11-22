import { describe, expect, it } from "vitest";
import {
  waitingGateStageId,
  canResume,
  canRetry,
  canCancel,
} from "./actions";

describe("waitingGateStageId", () => {
  it("returns the sole waiting_approval stage", () => {
    expect(
      waitingGateStageId([
        { id: "review", status: "succeeded" },
        { id: "approve_ship", status: "waiting_approval" },
      ]),
    ).toBe("approve_ship");
  });
  it("returns null when none or multiple", () => {
    expect(waitingGateStageId([{ id: "a", status: "succeeded" }])).toBeNull();
    expect(
      waitingGateStageId([
        { id: "a", status: "waiting_approval" },
        { id: "b", status: "waiting_approval" },
      ]),
    ).toBeNull();
  });
});

describe("canResume", () => {
  it("allows failed and cancelled", () => {
    expect(canResume("failed")).toBe(true);
    expect(canResume("cancelled")).toBe(true);
    expect(canResume("succeeded")).toBe(false);
  });
});

describe("canRetry", () => {
  it("allows failed and cancelled without force", () => {
    expect(canRetry("failed")).toBe(true);
    expect(canRetry("cancelled")).toBe(true);
    expect(canRetry("succeeded")).toBe(false);
    expect(canRetry("running")).toBe(false);
    expect(canRetry("blocked")).toBe(false);
  });
});

describe("canCancel", () => {
  it("allows running and blocked (waiting_approval run status)", () => {
    expect(canCancel("running")).toBe(true);
    expect(canCancel("blocked")).toBe(true);
    expect(canCancel("waiting_approval")).toBe(false);
    expect(canCancel("failed")).toBe(false);
    expect(canCancel("succeeded")).toBe(false);
    expect(canCancel("cancelled")).toBe(false);
  });
});
