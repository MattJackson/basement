import { useListClusters } from "@/shared/api/queries";

type BadgeSize = "sm" | "md";

interface ClusterBadgeProps {
  connectionId: string;
  size?: BadgeSize;
}

export function ClusterBadge({ connectionId, size = "md" }: ClusterBadgeProps) {
  const { data: connections } = useListClusters();

  const connection = connections?.find((c) => c.id === connectionId);
  const label = connection?.label ?? connectionId.slice(0, 8);
  const color = connection?.color ?? "#C9874B";

  const dotSize = size === "sm" ? "h-2.5 w-2.5" : "h-3 w-3";
  const textSize = size === "sm" ? "text-xs" : "text-sm";

  return (
    <div className="inline-flex items-center gap-1.5">
      <span
        className={`rounded-full ${dotSize}`}
        style={{ backgroundColor: color }}
        aria-hidden="true"
      />
      <span className={textSize}>{label}</span>
    </div>
  );
}
