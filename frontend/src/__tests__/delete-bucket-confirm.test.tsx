import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent } from "@testing-library/react";
import { DeleteBucketConfirm } from "@/shared/ui/DeleteBucketConfirm";

const BUCKET = "production-photos";

describe("DeleteBucketConfirm — destruction guard", () => {
  it("Delete button is disabled when the input is empty", () => {
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    const action = screen.getByTestId("delete-bucket-confirm-action");
    expect(action).toBeDisabled();
  });

  it("Delete button is disabled when the input is wrong", () => {
    const onConfirm = vi.fn();
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    fireEvent.change(screen.getByTestId("delete-bucket-confirm-input"), {
      target: { value: "production-photo" },
    });

    expect(screen.getByTestId("delete-bucket-confirm-action")).toBeDisabled();

    fireEvent.click(screen.getByTestId("delete-bucket-confirm-action"));
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("Delete button is disabled when the input is suffix-extended", () => {
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    fireEvent.change(screen.getByTestId("delete-bucket-confirm-input"), {
      target: { value: "production-photos-extra" },
    });

    expect(screen.getByTestId("delete-bucket-confirm-action")).toBeDisabled();
  });

  it("Delete button enables only on exact match (trim whitespace)", () => {
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    fireEvent.change(screen.getByTestId("delete-bucket-confirm-input"), {
      target: { value: `  ${BUCKET}  ` },
    });

    expect(screen.getByTestId("delete-bucket-confirm-action")).toBeEnabled();
  });

  it("clicking Delete calls onConfirm exactly once when match is exact", () => {
    const onConfirm = vi.fn();
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        onConfirm={onConfirm}
        onCancel={() => {}}
      />,
    );

    fireEvent.change(screen.getByTestId("delete-bucket-confirm-input"), {
      target: { value: BUCKET },
    });
    fireEvent.click(screen.getByTestId("delete-bucket-confirm-action"));

    expect(onConfirm).toHaveBeenCalledTimes(1);
  });

  it("disables Delete even on exact match while isDeleting is true", () => {
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        isDeleting
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    fireEvent.change(screen.getByTestId("delete-bucket-confirm-input"), {
      target: { value: BUCKET },
    });

    expect(screen.getByTestId("delete-bucket-confirm-action")).toBeDisabled();
  });

  it("Cancel calls onCancel and doesn't trigger onConfirm", () => {
    const onConfirm = vi.fn();
    const onCancel = vi.fn();
    render(
      <DeleteBucketConfirm
        open
        bucketAlias={BUCKET}
        onConfirm={onConfirm}
        onCancel={onCancel}
      />,
    );

    fireEvent.change(screen.getByTestId("delete-bucket-confirm-input"), {
      target: { value: BUCKET },
    });
    fireEvent.click(screen.getByRole("button", { name: /cancel/i }));

    expect(onCancel).toHaveBeenCalled();
    expect(onConfirm).not.toHaveBeenCalled();
  });

  it("resets the typed input when the dialog reopens for a different bucket", () => {
    const { rerender } = render(
      <DeleteBucketConfirm
        open
        bucketAlias="bucket-a"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    const input = screen.getByTestId(
      "delete-bucket-confirm-input",
    ) as HTMLInputElement;
    fireEvent.change(input, { target: { value: "bucket-a" } });
    expect(input.value).toBe("bucket-a");

    // Close + reopen on a different bucket
    rerender(
      <DeleteBucketConfirm
        open={false}
        bucketAlias="bucket-a"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );
    rerender(
      <DeleteBucketConfirm
        open
        bucketAlias="bucket-b"
        onConfirm={() => {}}
        onCancel={() => {}}
      />,
    );

    expect(
      (screen.getByTestId("delete-bucket-confirm-input") as HTMLInputElement)
        .value,
    ).toBe("");
  });
});
