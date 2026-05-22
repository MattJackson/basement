import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { useUserRegionBuckets } from "@/shared/api/queries";

interface UserRegionCardProps {
  id: string;
  alias: string;
  endpoint: string;
  region?: string;
  accessKeyId?: string;
}

// UserRegionCard renders a single UserRegion entry on /files. The card
// pulls live bucket counts via useUserRegionBuckets (signed against
// the region's own S3 key) so the header line stays honest without
// requiring a separate fan-out aggregator.
//
// Click anywhere on the card → /files/regions/$rid (bucket browser
// for this keychain entry).
export function UserRegionCard({ id, alias, endpoint, region, accessKeyId }: UserRegionCardProps) {
  const { data: buckets, isLoading, error } = useUserRegionBuckets(id);
  const bucketCount = buckets?.length ?? 0;

  return (
    <Link to="/files/regions/$rid" params={{ rid: id }} data-testid="user-region-card-link">
      <Card
        className="group/card cursor-pointer hover:shadow-md transition-shadow"
        data-testid="user-region-card"
      >
        <CardHeader>
          <CardTitle className="flex-1 truncate">{alias || endpoint}</CardTitle>
          <CardDescription className="space-y-1">
            <div className="truncate text-xs text-muted-foreground">{endpoint}</div>
            {region && (
              <div className="text-xs text-muted-foreground">region: {region}</div>
            )}
            {accessKeyId && (
              <div className="font-mono text-xs text-muted-foreground truncate">
                {accessKeyId}
              </div>
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          {error ? (
            <p className="text-sm text-destructive">
              Couldn&apos;t list buckets — key may have lost access.
            </p>
          ) : isLoading ? (
            <p className="text-sm text-muted-foreground tabular-nums">Loading...</p>
          ) : (
            <p className="text-sm text-muted-foreground tabular-nums">
              {bucketCount} bucket{bucketCount !== 1 ? "s" : ""}
            </p>
          )}
        </CardContent>
      </Card>
    </Link>
  );
}
