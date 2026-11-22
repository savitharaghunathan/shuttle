import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { MemoryRouter, Route, Routes, useParams } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import NewRun from "./NewRun";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

function DetailStub() {
  const { id } = useParams();
  return <div>Detail {id}</div>;
}

function renderForm() {
  return render(
    <MemoryRouter initialEntries={["/runs/new"]}>
      <Routes>
        <Route path="/runs/new" element={<NewRun />} />
        <Route path="/runs/:id" element={<DetailStub />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe("NewRun", () => {
  it("loads pipelines and posts exactly one of issue/spec", async () => {
    const fetchMock = vi.fn(
      async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/v1/pipelines") {
          return new Response(
            JSON.stringify({ pipelines: [{ id: "sdlc" }, { id: "demo" }] }),
            { status: 200, headers: { "Content-Type": "application/json" } },
          );
        }
        if (path === "/v1/runs" && init?.method === "POST") {
          return new Response(JSON.stringify({ id: "run-abc" }), {
            status: 201,
            headers: { "Content-Type": "application/json" },
          });
        }
        return new Response("not found", { status: 404 });
      },
    );
    vi.stubGlobal("fetch", fetchMock);

    renderForm();

    expect(
      await screen.findByRole("heading", { name: "New run" }),
    ).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.getByRole("combobox")).toHaveValue("sdlc");
    });

    fireEvent.click(screen.getByRole("button", { name: "Create run" }));
    expect(
      screen.getByText(/exactly one of issue URL or absolute spec path/i),
    ).toBeInTheDocument();

    fireEvent.change(screen.getByPlaceholderText(/github.com/i), {
      target: { value: "https://github.com/acme/widget/issues/1" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create run" }));

    expect(await screen.findByText("Detail run-abc")).toBeInTheDocument();

    const postCall = fetchMock.mock.calls.find(
      ([url, init]) => String(url) === "/v1/runs" && init?.method === "POST",
    );
    expect(postCall).toBeTruthy();
    const body = JSON.parse(String(postCall![1]?.body));
    expect(body).toMatchObject({
      template_id: "sdlc",
      issue_url: "https://github.com/acme/widget/issues/1",
      headless: false,
      remote: false,
    });
    expect(body.spec_path).toBeUndefined();
  });

  it("shows API error inline and re-enables submit", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
        const path = String(input);
        if (path === "/v1/pipelines") {
          return new Response(JSON.stringify({ pipelines: [{ id: "demo" }] }), {
            status: 200,
            headers: { "Content-Type": "application/json" },
          });
        }
        if (path === "/v1/runs" && init?.method === "POST") {
          return new Response(JSON.stringify({ error: "boom" }), {
            status: 400,
            headers: { "Content-Type": "application/json" },
          });
        }
        return new Response("not found", { status: 404 });
      }),
    );

    renderForm();
    await screen.findByRole("combobox");
    fireEvent.change(screen.getByPlaceholderText(/github.com/i), {
      target: { value: "https://github.com/acme/widget/issues/2" },
    });
    fireEvent.click(screen.getByRole("button", { name: "Create run" }));

    expect(await screen.findByText("boom")).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Create run" }),
    ).not.toBeDisabled();
  });
});
