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

import type { WebhookMintResponse } from "@/shared/api/queries";

// v1.7.0e — shown-once dialog for webhook secret mint + rotate.
//
// Sibling of SecretShownOnceDialog (service accounts) — same contract
// (`dismissible=false`, checkbox-gated Done, reveal/copy controls) but
// the payload is webhook-shaped and the copy snippet teaches the
// operator how to verify the HMAC on the receiving side instead of how
// to mint an aws-cli profile.
//
// The signing scheme is HMAC-SHA256 with hex output, surfaced in the
// X-Basement-Signature header as "sha256=<hex>". See
// internal/webhook/engine.go + signWebhookBody for the wire spec.

export function WebhookSecretShownOnceDialog({
  result,
  mode,
  onDone,
}: {
  result: WebhookMintResponse;
  mode: "create" | "rotate";
  onDone: () => void;
}) {
  const [revealed, setRevealed] = useState(false);
  const [acknowledged, setAcknowledged] = useState(false);

  const secret = result.secret;
  const name = result.name;

  // Verification snippet — Python because it's the lowest-friction
  // language to drop into a webhook receiver and it reads cleanly even
  // for operators who do most of their plumbing in another stack. The
  // value is parameterised: YOUR-SECRET is the placeholder for the
  // operator's just-saved secret, request.body is whatever shape their
  // framework gives them for the raw bytes.
  const verifySnippet = `# Verify the signature in your webhook receiver:
import hmac, hashlib

# X-Basement-Signature looks like "sha256=<hex>" — strip the prefix.
sig = request.headers['X-Basement-Signature'].split('=')[1]
expected = hmac.new(
    b'YOUR-SECRET',
    request.body,
    hashlib.sha256,
).hexdigest()
assert hmac.compare_digest(sig, expected)`;

  const title =
    mode === "rotate"
      ? "Save your new webhook secret — it won't be shown again"
      : "Save your webhook secret — it won't be shown again";

  const intro =
    mode === "rotate"
      ? `The secret for ${name} has been rotated. Deliveries from this point forward sign with the new value; the previous secret is invalid effective immediately. Copy the new secret now — we can't show it again later.`
      : `The backend returns the secret for ${name} exactly once. Copy it now and paste it into your webhook receiver alongside the verification snippet below. We can't show it again later.`;

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
          <div
            role="alert"
            className="rounded-md border-2 border-amber-300 dark:border-amber-700 bg-amber-50 dark:bg-amber-950/30 px-3 py-2 text-sm text-amber-900 dark:text-amber-200"
          >
            <strong>This secret will not be shown again.</strong> Save it
            now — once you click Done the plaintext is dropped from
            memory and no read path on the server can recover it.
          </div>

          <FieldRow label="Webhook name" value={name} testId="wh-name" />

          <SecretRow
            label="Signing secret"
            value={secret}
            revealed={revealed}
            onToggleReveal={() => setRevealed((v) => !v)}
            testId="wh-secret"
          />

          <FieldRow
            label="HMAC verification snippet (Python)"
            value={verifySnippet}
            testId="wh-verify-snippet"
            multiline
          />

          <label
            className="flex items-start gap-2 pt-2 cursor-pointer select-none"
            data-testid="wh-acknowledged-label"
          >
            <Checkbox
              checked={acknowledged}
              onCheckedChange={(c) => setAcknowledged(c === true)}
              data-testid="wh-acknowledged-checkbox"
            />
            <span className="text-sm">
              I've copied the secret into my webhook receiver or a
              password manager. I understand it will not be shown
              again.
            </span>
          </label>
        </div>

        <DialogFooter>
          <Button
            type="button"
            onClick={onDone}
            disabled={!acknowledged}
            data-testid="wh-done-button"
          >
            Done
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

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

function copyToClipboard(value: string) {
  try {
    void navigator.clipboard.writeText(value);
  } catch {
    // Clipboard API can be unavailable in some sandboxed iframes; the
    // visible text remains the source of truth.
  }
}
