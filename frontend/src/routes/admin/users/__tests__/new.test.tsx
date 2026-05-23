// Tests for the /admin/users/new "New user" form.
//
// v1.10.0.2 — adds inline-validation coverage that mirrors the same
// pattern v1.10.0.1 introduced for /files/keys/new. Pre-fix the
// "Create user" button was simply `disabled` while the username was
// blank — an operator clicking a pristine form got no feedback at all
// (smoke section [C] caught it). The new behaviour: submit stays
// enabled, click fires per-field validation, inline role="alert"
// errors render next to each blank required input, and the user stays
// on the page.

const navigateMock = vi.fn();
const postMock = vi.fn();

vi.mock("@tanstack/react-router", async (importOriginal) => {
  const actual = await importOriginal();
  return {
    ...(actual as object),
    createFileRoute: () => () => ({}),
    useNavigate: () => navigateMock,
    Link: ({ children, ...rest }: { children: React.ReactNode } & Record<string, unknown>) => (
      <a {...rest}>{children}</a>
    ),
  };
});

vi.mock("@/shared/api/client", () => ({
  client: {
    POST: (...args: unknown[]) => postMock(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import NewUserPage from "@/routes/admin/users/new";

beforeEach(() => {
  vi.clearAllMocks();
  navigateMock.mockReset();
  postMock.mockReset();
});

describe("NewUserPage (/admin/users/new)", () => {
  it("surfaces inline validation errors on blank submit (does not call API)", async () => {
    render(<NewUserPage />);

    const submit = screen.getByRole("button", { name: /Create user/i });
    // v1.10.0.2 — submit is no longer disabled while username is blank.
    expect(submit).toBeEnabled();

    await userEvent.click(submit);

    // Inline role="alert" surfaces for the blank username.
    const alerts = await screen.findAllByRole("alert");
    expect(alerts.length).toBeGreaterThanOrEqual(1);

    // The offending input flips aria-invalid.
    const usernameInput = screen.getByLabelText(/Username/);
    expect(usernameInput).toHaveAttribute("aria-invalid", "true");

    // Mutation never fires + we don't navigate away.
    expect(postMock).not.toHaveBeenCalled();
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it("surfaces a password-length error when invite-only is off and password is too short", async () => {
    render(<NewUserPage />);

    await userEvent.type(screen.getByLabelText(/Username/), "alice");
    await userEvent.type(screen.getByLabelText(/Password/), "short");
    await userEvent.click(screen.getByRole("button", { name: /Create user/i }));

    const passwordInput = screen.getByLabelText(/Password/);
    expect(passwordInput).toHaveAttribute("aria-invalid", "true");

    // Inline alert text mentions the 8-char rule.
    const alert = await screen.findByRole("alert");
    expect(alert.textContent ?? "").toMatch(/8 char/i);

    expect(postMock).not.toHaveBeenCalled();
    expect(navigateMock).not.toHaveBeenCalled();
  });

  it("clears the username error when the operator starts typing", async () => {
    render(<NewUserPage />);

    await userEvent.click(screen.getByRole("button", { name: /Create user/i }));
    expect(screen.getByLabelText(/Username/)).toHaveAttribute("aria-invalid", "true");

    await userEvent.type(screen.getByLabelText(/Username/), "a");
    expect(screen.getByLabelText(/Username/)).not.toHaveAttribute("aria-invalid");
    // The username-specific inline alert (id="username-error") disappears.
    expect(document.getElementById("username-error")).toBeNull();
  });
});
