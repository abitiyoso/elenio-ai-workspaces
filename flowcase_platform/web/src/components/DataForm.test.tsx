import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { DataForm } from "./DataForm";

describe("DataForm", () => {
  it("does not render properties marked x-hidden", () => {
    render(
      <DataForm idPrefix="t" values={{}} onChange={() => {}} schema={{
        type: "object", required: ["deadline"],
        properties: {
          deadline: { type: "string", format: "date-time", title: "Deadline" },
          customer_id: { type: "string", title: "Customer ID", "x-hidden": true },
          customer_name: { type: "string", title: "Customer name" },
        },
      }} />,
    );
    expect(screen.getByLabelText("Deadline *")).toBeInTheDocument();
    expect(screen.getByLabelText("Customer name")).toBeInTheDocument();
    expect(screen.queryByLabelText(/Customer ID/)).not.toBeInTheDocument();
  });
});
