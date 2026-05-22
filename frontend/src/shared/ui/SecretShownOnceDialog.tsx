import { useState } from "react";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/checkbox";

import type { ServiceAccountWithSecret } from "@/shared/api/queries";
import { McpConfigSection } from "@/shared/ui/McpConfigSection";

// v1.7.0c — shared shown-once dialog for service-account mint + rotate.
//
// Mirrors the v0.9.0m Garage-key reveal flow on /admin/keys: dismissible
// is false (Escape + outside-click won't drop the only copy), the secret
// can be revealed/hidden, and a "saved" checkbox gates the Done button
// so the operator must affirmatively acknowledge they've copied it.
//
// Per the cycle prompt, we surface FOUR copy paths:
//   - Access key ID alone
//   - Secret alone
//   - The bearer header (`Authorization: Bearer AKID:SECRET`) — the
//     primary v1.7.0b usage shape
//   - An aws-cli credentials-file snippet — for the v2.0 SigV4 gateway
//     and S3-compat clients that read ~/.aws/credentials
//
// The Done button only fires after explicit click (no auto-close on
// outside / escape; checkbox-gated).
export function SecretShownOnceDialog({
  result,
  mode,
  onDone,
}: {
  result: ServiceAccountWithSecret;
  mode: "create" | "rotate";
  onDone: () => void;
}) {
  const [revealed, setRevealed] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);

  const { serviceAccount: sa, secret } = result;
  const akid = sa.accessKeyId;

  const bearerHeader = `Authorization: Bearer ${akid}:${secret}`;
  const awsProfile = profileNameFor(sa.name);
  const awsSnippet = `[${awsProfile}]
aws_access_key_id = ${akid}
aws_secret_access_key = ${secret}`;

  const title =
    mode === "rotate"
      ? "Save your new secret — it won't be shown again"
      : "Save your access key — it won't be shown again";
  const intro =
    mode === "rotate"
      ? `The secret for ${sa.name} has been rotated. The previous secret is invalid effective immediately. Copy the new secret now — we can't show it again later.`
      : `The backend returns the secret for ${sa.name} exactly once. Copy it now, then store it in your password manager or CI secret store. We can't show it again later.`;

  return (
    <Dialog
      open={true}
      dismissible={false}
      onOpenChange={() => {
        /* explicit confirm only — onDone fires on Done click */
      }}
    >
      <DialogContent className="max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
          <DialogDescription>{intro}</DialogDescription>
        </DialogHeader>

        <div className="space-y-3">
          {/* Big alert — reinforces the shown-once contract for anyone
              who skims past the description. */}
          <div
            role="alert"
            className="rounded-md border-2 border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 text-sm text-amber-900 dark:text-amber-200"
          >
            <strong>This secret will not be shown again.</strong> Save it
            now — once you click Done the plaintext is dropped from
            memory and no read path on the server can recover it.
          </div>

          <FieldRow
            label="Access Key ID"
            value={akid}
            testId="sa-akid"
          />

          <SecretRow
            label="Secret"
            value={secret}
            revealed={revealed}
            onToggleReveal={() => setRevealed((v) => !v)}
            testId="sa-secret"
          />

          <FieldRow
            label="Bearer header"
            value={bearerHeader}
            testId="sa-bearer"
            multiline
          />

          <FieldRow
            label="~/.aws/credentials snippet"
            value={awsSnippet}
            testId="sa-aws-snippet"
            multiline
          />

          {/* v1.8.0d — Drop the same service-account into a basement-mcp
              profile so AI clients can authenticate as this SA. The
              plaintext secret is available here exactly once, so the
              YAML inlines it; the detail page surfaces the same
              section with a <SECRET_FROM_ROTATE> placeholder. */}
          <McpConfigSection accessKeyId={akid} secret={secret} />

          <label
            className="flex items-start gap-2 pt-2 cursor-pointer select-none"
            data-testid="sa-acknowledged-label"
          >
            <Checkbox
              checked={acknowledged}
              onCheckedChange={(c) => setAcknowledged(c === true)}
              data-testid="sa-acknowledged-checkbox"
            />
            <span className="text-sm">
              I've copied the secret into a password manager or CI
              secret store. I understand it will not be shown again.
            </span>
          </label>
        </div>

        <DialogFooter>
          <Button
            type="button"
            onClick={onDone}
            disabled={!acknowledged}
            data-testid="sa-done-button"
          >
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

// FieldRow renders a label + read-only value + Copy button. Used for
// plain (non-sensitive) and the always-visible composite fields like
// the bearer header + aws snippet.
function FieldRow({
  label,
  value,
  testId,
  multiline = false,
}: {
  label: string;
  value: string;
  testId: string;
  multiline?: boolean;
}) {
  return (
    <div>
      <div className="text-xs font-medium text-muted-foreground mb-1">
        {label}
      </div>
      <div className="flex gap-2">
        {multiline ? (
          <pre
            className="flex-1 rounded bg-muted px-3 py-2 font-mono text-xs whitespace-pre-wrap break-all"
            data-testid={testId}
          >
            {value}
          </pre>
        ) : (
          <code
            className="flex-1 rounded bg-muted px-3 py-2 font-mono text-sm break-all"
            data-testid={testId}
          >
            {value}
          </code>
        )}
        <Button
          variant="outline"
          size="sm"
          onClick={() => copyToClipboard(value)}
          data-testid={`${testId}-copy`}
        >
          Copy
        </Button>
      </div>
    </div>
  );
}

function SecretRow({
  label,
  value,
  revealed,
  onToggleReveal,
  testId,
}: {
  label: string;
  value: string;
  revealed: boolean;
  onToggleReveal: () => void;
  testId: string;
}) {
  return (
    <div>
      <div className="text-xs font-medium text-amber-700 dark:text-amber-400 mb-1">
        {label} — shown once
      </div>
      <div className="flex gap-2">
        <code
          className="flex-1 rounded border-2 border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 font-mono text-sm break-all"
          data-testid={testId}
        >
          {revealed ? value : "•".repeat(Math.min(value.length, 48))}
        </code>
        <Button
          variant="outline"
          size="sm"
          onClick={onToggleReveal}
          data-testid={`${testId}-reveal`}
        >
          {revealed ? "Hide" : "Show"}
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={() => copyToClipboard(value)}
          data-testid={`${testId}-copy`}
        >
          Copy
        </Button>
      </div>
    </div>
  );
}

// copyToClipboard wraps the async clipboard call so callers can stay
// onClick-shaped without async/await. Failure is silent because the
// shown-once dialog is exactly the wrong place to throw — the user
// can still read the on-screen value and copy by hand.
function copyToClipboard(value: string) {
  try {
    void navigator.clipboard.writeText(value);
  } catch {
    // Clipboard API can be unavailable in some sandboxed iframes; the
    // visible text remains the source of truth.
  }
}

// profileNameFor sanitises the SA name into something safe for an
// aws-cli profile section header. Same rule the /admin/keys flow uses
// for its snippet: lowercase, keep [a-z0-9_-], collapse leading /
// trailing dashes. Falls back to "basement" if the sanitiser empties
// the string.
function profileNameFor(name: string): string {
  const cleaned = name
    .toLowerCase()
    .replace(/[^a-z0-9_-]+/g, "-")
    .replace(/^-+|-+$/g, "");
  return cleaned || "basement";
}
