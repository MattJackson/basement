import { useState } from "react";
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Button } from "@/components/ui/button";
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group";
import { Checkbox } from "@/components/ui/checkbox";
import { useCreateUserShare, useRevokeUserShare } from "@/shared/api/queries";

interface CreateShareDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  defaultConnectionId?: string;
  defaultBucketId?: string;
  defaultPrefix?: string;
  defaultKey?: string;
}

export function CreateShareDialog({
  open,
  onOpenChange,
  defaultConnectionId,
  defaultBucketId,
  defaultPrefix,
  defaultKey,
}: CreateShareDialogProps) {
  const [shareType, setShareType] = useState<"prefix" | "object">(defaultPrefix ? "prefix" : "object");
  const [connectionId, setConnectionId] = useState(defaultConnectionId || "");
  const [bucketId, setBucketId] = useState(defaultBucketId || "");
  const [prefix, setPrefix] = useState(defaultPrefix || "");
  const [key, setKey] = useState(defaultKey || "");
  const [expiresIn, setExpiresIn] = useState<"24h" | "7d" | "30d" | "never">("7d");
  const [downloadLimit, setDownloadLimit] = useState<string>("");
  const [hasPassword, setHasPassword] = useState(false);
  const [password, setPassword] = useState("");

  const createShare = useCreateUserShare();
  const revokeShare = useRevokeUserShare();

  const generatedLink = createShare.data ? `https://basement.example.com/share/${createShare.data.token}` : null;

  const handleSave = () => {
    if (!connectionId || !bucketId) return;
    
    let payload: {
      connectionId: string;
      bucketId: string;
      prefix?: string;
      key?: string;
      expiresAt?: string;
      downloadLimit?: number;
      password?: string;
    } = {
      connectionId,
      bucketId,
    };

    if (shareType === "prefix") {
      payload.prefix = prefix || "";
    } else {
      payload.key = key || "";
    }

    const now = new Date();
    let expiresDate: Date | undefined;
    
    switch (expiresIn) {
      case "24h":
        expiresDate = new Date(now.getTime() + 24 * 60 * 60 * 1000);
        break;
      case "7d":
        expiresDate = new Date(now.getTime() + 7 * 24 * 60 * 60 * 1000);
        break;
      case "30d":
        expiresDate = new Date(now.getTime() + 30 * 24 * 60 * 60 * 1000);
        break;
      default:
        expiresDate = undefined;
    }

    if (expiresDate) {
      payload.expiresAt = expiresDate.toISOString();
    }

    if (downloadLimit.trim()) {
      const limit = parseInt(downloadLimit, 10);
      if (!isNaN(limit) && limit > 0) {
        payload.downloadLimit = limit;
      }
    }

    if (hasPassword && password.length >= 8) {
      payload.password = password;
    }

    createShare.mutate(payload, {
      onSuccess: () => {
        // Keep dialog open to show generated link
      },
    });
  };

  const copyLink = async () => {
    if (generatedLink) {
      await navigator.clipboard.writeText(generatedLink);
    }
  };

  const handleRevoke = () => {
    if (!createShare.data || !confirm("This will revoke the share link. Recipients will no longer be able to access it.")) {
      return;
    }
    revokeShare.mutate(createShare.data.token, {
      onSuccess: () => {
        onOpenChange(false);
      },
    });
  };

  const resetForm = () => {
    setShareType(defaultPrefix ? "prefix" : "object");
    setConnectionId("");
    setBucketId("");
    setPrefix("");
    setKey("");
    setExpiresIn("7d");
    setDownloadLimit("");
    setHasPassword(false);
    setPassword("");
  };

  const isCreateDisabled = !connectionId || !bucketId || (shareType === "prefix" && !prefix) || (shareType === "object" && !key) || createShare.isPending;

  return (
    <Dialog open={open} onOpenChange={(isOpen) => {
      if (!isOpen) resetForm();
      onOpenChange(isOpen);
    }}>
      <DialogContent className="sm:max-w-[600px]">
        {!generatedLink ? (
          <>
            <DialogHeader>
              <DialogTitle>Create Share Link</DialogTitle>
              <DialogDescription>Create a shareable link for a bucket prefix or single object.</DialogDescription>
            </DialogHeader>

            <div className="grid gap-4 py-4">
              <RadioGroup value={shareType} onValueChange={(v) => setShareType(v as "prefix" | "object")}>
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="prefix" id="prefix-option" />
                  <Label htmlFor="prefix-option">Share a prefix (folder)</Label>
                </div>
                <div className="flex items-center space-x-2">
                  <RadioGroupItem value="object" id="object-option" />
                  <Label htmlFor="object-option">Share a single object</Label>
                </div>
              </RadioGroup>

              {shareType === "prefix" ? (
                <div className="grid gap-2">
                  <Label htmlFor="prefix-input">Prefix path</Label>
                  <Input
                    id="prefix-input"
                    value={prefix}
                    onChange={(e) => setPrefix(e.target.value)}
                    placeholder="path/to/folder/"
                  />
                </div>
              ) : (
                <div className="grid gap-2">
                  <Label htmlFor="key-input">Object key</Label>
                  <Input
                    id="key-input"
                    value={key}
                    onChange={(e) => setKey(e.target.value)}
                    placeholder="path/to/single-file.txt"
                  />
                </div>
              )}

              <div className="grid gap-2">
                <Label htmlFor="expires-in">Expiration</Label>
                <RadioGroup value={expiresIn} onValueChange={(v) => setExpiresIn(v as "24h" | "7d" | "30d" | "never")}>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem value="24h" id="exp-24h" />
                    <Label htmlFor="exp-24h">24 hours</Label>
                  </div>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem value="7d" id="exp-7d" />
                    <Label htmlFor="exp-7d">7 days</Label>
                  </div>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem value="30d" id="exp-30d" />
                    <Label htmlFor="exp-30d">30 days</Label>
                  </div>
                  <div className="flex items-center space-x-2">
                    <RadioGroupItem value="never" id="exp-never" />
                    <Label htmlFor="exp-never">Never expire</Label>
                  </div>
                </RadioGroup>
              </div>

              <div className="grid gap-2">
                <Label htmlFor="download-limit">Download limit (optional)</Label>
                <Input
                  id="download-limit"
                  value={downloadLimit}
                  onChange={(e) => setDownloadLimit(e.target.value)}
                  placeholder="e.g., 10"
                  type="number"
                  min="1"
                />
              </div>

              <div className="flex items-center space-x-2">
                <Checkbox id="password-checkbox" checked={hasPassword} onCheckedChange={(v) => setHasPassword(!!v)} />
                <Label htmlFor="password-checkbox">Protect with password</Label>
              </div>

              {hasPassword && (
                <div className="grid gap-2">
                  <Label htmlFor="password-input">Password</Label>
                  <Input
                    id="password-input"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    placeholder="Minimum 8 characters"
                    type="password"
                  />
                </div>
              )}
            </div>

            <DialogFooter>
              <Button variant="outline" onClick={() => onOpenChange(false)}>Cancel</Button>
              <Button onClick={handleSave} disabled={isCreateDisabled}>Create Link</Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Your Share Link is Ready!</DialogTitle>
              <DialogDescription>This link will be shown only once. Save it now.</DialogDescription>
            </DialogHeader>

            <div className="grid gap-4 py-4">
              <div className="rounded-md bg-muted p-4 space-y-2">
                <p className="text-sm text-muted-foreground">Share URL:</p>
                <div className="flex items-center gap-2">
                  <code className="flex-1 rounded bg-background px-3 py-2 text-sm font-mono break-all">{generatedLink}</code>
                  <Button variant="outline" size="sm" onClick={copyLink}>Copy</Button>
                </div>
              </div>

              {createShare.data?.passwordHash && (
                <div className="rounded-md bg-amber-50 border border-amber-200 p-3 text-sm">
                  <p className="font-medium text-amber-800 mb-1">Password protection enabled</p>
                  <p className="text-amber-700">Make sure to share the password securely with recipients.</p>
                </div>
              )}

              {createShare.data?.downloadLimit && (
                <div className="rounded-md bg-blue-50 border border-blue-200 p-3 text-sm">
                  <p className="font-medium text-blue-800 mb-1">Download limit: {createShare.data.downloadLimit}</p>
                  <p className="text-blue-700">This link will stop working after {createShare.data.downloadLimit} downloads.</p>
                </div>
              )}

              {createShare.data?.expiresAt && (
                <div className="rounded-md bg-red-50 border border-red-200 p-3 text-sm">
                  <p className="font-medium text-red-800 mb-1">Expires: {new Date(createShare.data.expiresAt).toLocaleString()}</p>
                  <p className="text-red-700">This link will stop working after the expiration date.</p>
                </div>
              )}

              <div className="rounded-md bg-orange-50 border border-orange-200 p-3 text-sm">
                <p className="font-medium text-orange-800 mb-1">⚠️ Important</p>
                <p className="text-orange-700">You won't be able to see this link again. Save it somewhere safe.</p>
              </div>

              {createShare.data && (
                <Button variant="outline" onClick={handleRevoke} disabled={revokeShare.isPending}>
                  Revoke This Link
                </Button>
              )}
            </div>

            <DialogFooter>
              <Button onClick={() => onOpenChange(false)}>Done</Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

export default CreateShareDialog;
