import { Badge } from "@/components/ui/badge";

export interface LockBadgeProps {
  unlocked: boolean;
  hasCsk: boolean;
  requiresMigration?: boolean;
}

/**
 * LockBadge — single-glance status pill for per-cluster envelope
 * encryption (ADR-0007 / v1.12.0a).
 *
 * Three logical states:
 *   1. No CSK at all — "Unencrypted" (the cluster has never had a CSK admin)
 *   2. CSK + unlocked — "Unlocked" (operations work normally)
 *   3. CSK + locked — "Locked" (mutations needing secrets return 423)
 *
 * Migration banner is layered on by the cluster detail screen
 * separately — keeping the badge to one word per state keeps the
 * header from getting noisy.
 */
export function LockBadge({ unlocked, hasCsk }: LockBadgeProps) {
  if (!hasCsk) {
    return (
      <Badge
        variant="outline"
        className="border-amber-300 bg-amber-50 text-amber-900"
        title="Cluster has no envelope-encryption admin yet. Stored secrets are protected by the deployment-wide JWT key only."
      >
        Unencrypted
      </Badge>
    );
  }
  if (unlocked) {
    return (
      <Badge
        variant="outline"
        className="border-green-300 bg-green-50 text-green-900"
        title="Cluster Secret Key is in memory. Background jobs can decrypt stored secrets."
      >
        Unlocked
      </Badge>
    );
  }
  return (
    <Badge
      variant="outline"
      className="border-red-300 bg-red-50 text-red-900"
      title="Cluster Secret Key is not in memory. Mutations that need stored secrets will prompt for the cluster admin password."
    >
      Locked
    </Badge>
  );
}
