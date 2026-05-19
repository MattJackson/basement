import { describe, it, expect } from "vitest";
import { render, screen } from "@testing-library/react";
import { EmptyState } from "../EmptyState";

describe("EmptyState", () => {
  it("renders title and description correctly", () => {
    render(
      <EmptyState
        icon="database"
        title="No buckets created yet"
        description="Create your first storage bucket to get started."
      />
    );

    expect(screen.getByText("No buckets created yet")).toBeInTheDocument();
    expect(screen.getByText("Create your first storage bucket to get started.")).toBeInTheDocument();
  });

  it("renders database icon when icon prop is 'database'", () => {
    render(
      <EmptyState
        icon="database"
        title="No data"
        description="Description text"
      />
    );

    const svg = screen.getByLabelText(/database/i);
    expect(svg).toBeInTheDocument();
  });

  it("renders key icon when icon prop is 'key'", () => {
    render(
      <EmptyState
        icon="key"
        title="No keys"
        description="Description text"
      />
    );

    const svg = screen.getByLabelText(/key/i);
    expect(svg).toBeInTheDocument();
  });

  it("renders server icon when icon prop is 'server'", () => {
    render(
      <EmptyState
        icon="server"
        title="No nodes"
        description="Description text"
      />
    );

    const svg = screen.getByLabelText(/server/i);
    expect(svg).toBeInTheDocument();
  });

  it("renders alert-circle icon when icon prop is 'alert-circle'", () => {
    render(
      <EmptyState
        icon="alert-circle"
        title="No data"
        description="Description text"
      />
    );

    const svg = screen.getByLabelText(/alert-circle/i);
    expect(svg).toBeInTheDocument();
  });

  it("renders action element when provided", () => {
    render(
      <EmptyState
        icon="database"
        title="No buckets"
        description="Description text"
        action={<button>Create bucket</button>}
      />
    );

    expect(screen.getByText("Create bucket")).toBeInTheDocument();
  });

  it("renders without icon when no icon prop provided", () => {
    render(
      <EmptyState
        title="Empty state"
        description="Description text"
      />
    );

    const heading = screen.getByText("Empty state");
    expect(heading).toBeInTheDocument();
  });
});
