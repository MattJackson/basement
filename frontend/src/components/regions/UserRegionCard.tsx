import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { humanizeTime } from "@/shared/lib/format";
import type { UserRegion } from "@/shared/api/queries";

// UserRegionCard renders one row on the /files home page (ADR-0002,
// v1.1.0c). Replaces UserClusterCard. Click navigates to the region's
// bucket list, which lazy-fetches ListBuckets via the user's key.
interface UserRegionCardProps {
  region: UserRegion;
}

export function UserRegionCard({ region }: UserRegionCardProps) {
  return (
    <Link
      to="/files/$regionId"
      params={{ regionId: region.id }}
      data-testid="user-region-card-link"
    >
      <Card
        className="group/card cursor-pointer hover:shadow-md transition-shadow"
        data-testid="user-region-card"
      >
        <CardHeader>
          <div className="flex items-start gap-3">
            <span
              data-testid="region-dot"
              className="inline-block h-3 w-3 rounded-full border border-border flex-shrink-0 mt-1"
              style={{ backgroundColor: "#C9874B" }}
              aria-hidden
            />
            <div className="flex-1 min-w-0">
              <CardTitle className="truncate">{region.alias}</CardTitle>
              <CardDescription className="truncate">
                {hostnameOf(region.endpoint)}
              </CardDescription>
            </div>
          </div>
        </CardHeader>
        <CardContent>
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
