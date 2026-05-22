import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader } from "@/components/ui/card";
import { humanizeTime } from "@/shared/lib/format";
import type { UserRegion } from "@/shared/api/queries";

// UserKeyCard renders one card on the /files home page (ADR-0002,
// v1.2.0d). Replaces UserRegionCard.
//
// Per the v1.2.0d key-first model: each card is one ACCESS KEY in the
// user's keychain. The alias is the dominant label (a user may have
// multiple keys against the same endpoint with different aliases —
// e.g. "Work S3" + "Personal S3"), with the endpoint hostname as a
// small mono subtitle so the user can see WHICH service the alias
// points at, and the access key ID truncated below for at-a-glance
// confirmation that the right key is being used.
interface UserKeyCardProps {
  region: UserRegion;
}

export function UserKeyCard({ region }: UserKeyCardProps) {
  const alias = region.alias?.trim() || "(unnamed key)";
  const akPreview = region.accessKeyId
    ? region.accessKeyId.length > 12
      ? `${region.accessKeyId.slice(0, 12)}…`
      : region.accessKeyId
    : "";

  return (
    <Link
      to="/files/$regionId"
      params={{ regionId: region.id }}
      data-testid="user-key-card-link"
    >
      <Card
        className="group/card cursor-pointer hover:shadow-md transition-shadow"
        data-testid="user-key-card"
      >
        <CardHeader>
          <div className="flex items-start gap-3">
            <span
              data-testid="region-dot"
              className="inline-block h-3 w-3 rounded-full border border-border flex-shrink-0 mt-1.5"
              style={{ backgroundColor: "#C9874B" }}
              aria-hidden
            />
            <div className="flex-1 min-w-0">
              <div
                data-testid="user-key-card-alias"
                className="text-lg font-semibold tracking-tight truncate"
              >
                {alias}
              </div>
              <div
                data-testid="user-key-card-endpoint"
                className="text-xs font-mono text-muted-foreground truncate"
              >
                via {hostnameOf(region.endpoint)}
              </div>
            </div>
          </div>
        </CardHeader>
        <CardContent className="space-y-1">
          {akPreview && (
            <p
              data-testid="user-key-card-access-key"
              className="text-[11px] font-mono text-muted-foreground truncate"
            >
              {akPreview}
            </p>
          )}
          <p className="text-xs text-muted-foreground">
            {region.lastUsedAt
              ? `Last used ${humanizeTime(region.lastUsedAt)}`
              : "Never used"}
          </p>
        </CardContent>
      </Card>
    </Link>
  );
}

// hostnameOf strips scheme/path so the card stays compact. Falls back
// to the raw endpoint string if parsing fails.
function hostnameOf(endpoint: string): string {
  try {
    return new URL(endpoint).host;
  } catch {
    return endpoint;
  }
}
