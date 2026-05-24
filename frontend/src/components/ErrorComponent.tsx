import * as React from "react";

const SHOW_BY_DEFAULT = import.meta.env.DEV;

export function ErrorComponent({ error }: { error: any }) {
  const [show, setShow] = React.useState(SHOW_BY_DEFAULT);

  return (
    <div className="w-full rounded-lg border bg-destructive/10 p-4">
      <div className="flex items-center gap-2">
        <strong>Something went wrong!</strong>
        <button
          type="button"
          onClick={() => setShow((d) => !d)}
          className="inline-flex min-h-[44px] min-w-[44px] items-center justify-center rounded border bg-background px-3 py-2 text-sm font-medium hover:bg-accent focus:outline-none focus:ring-2 focus:ring-ring focus:ring-offset-2 sm:min-h-auto sm:min-w-auto"
        >
          {show ? "Hide Error" : "Show Error"}
        </button>
      </div>
      <div className="my-2 h-px bg-border" />
      {show ? (
        <pre className="overflow-auto rounded border bg-destructive/5 p-3 text-xs text-destructive">
          {error?.message ? <code>{error.message}</code> : null}
        </pre>
      ) : null}
    </div>
  );
}
