import { Link } from "@tanstack/react-router";
import { Card, CardContent, CardHeader, CardTitle, CardDescription } from "@/components/ui/card";
import { DriverBadge } from "@/components/clusters/DriverBadge";
import { useUserClusterBuckets } from "@/shared/api/queries";

interface UserClusterCardProps {
  id: string;
  label: string;
  driver: string;
  color?: string;
}

export function UserClusterCard({ id, label, driver, color }: UserClusterCardProps) {
  const { data: buckets } = useUserClusterBuckets(id);
  const bucketCount = buckets?.length ?? 0;

  return (
    <Link to="/files/$cid" params={{ cid: id }} data-testid="user-cluster-card-link">
      <Card
        className="group/card cursor-pointer hover:shadow-md transition-shadow"
        data-testid="user-cluster-card"
      >
        <CardHeader>
          <div className="flex items-start gap-3">
            <span
              data-testid="color-dot"
              className="inline-block h-3 w-3 rounded-full border border-border flex-shrink-0"
              style={{ backgroundColor: color ?? "#C9874B" }}
              aria-hidden
            />
            <CardTitle className="flex-1">{label}</CardTitle>
          </div>
          <CardDescription className="flex items-center gap-2">
            <DriverBadge driver={driver} />
          </CardDescription>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground tabular-nums">
            {bucketCount} bucket{bucketCount !== 1 ? "s" : ""}
          </p>
        </CardContent>
      </Card>
    </Link>
  );
}
