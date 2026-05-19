import { Badge } from "@/components/ui/badge";

type HealthStatus = "healthy" | "degraded" | "unavailable";

interface HealthPillProps {
  status: HealthStatus;
}

export function HealthPill({ status }: HealthPillProps) {
  const variants = {
    healthy: "bg-green-500/10 text-green-600 dark:text-green-400 border-green-500/20",
    degraded: "bg-yellow-500/10 text-yellow-600 dark:text-yellow-400 border-yellow-500/20",
    unavailable: "bg-red-500/10 text-red-600 dark:text-red-400 border-red-500/20",
  } as const;

  const labels = {
    healthy: "Healthy",
    degraded: "Degraded",
    unavailable: "Unavailable",
  } as const;

  return (
    <Badge variant="outline" className={`${variants[status]} px-3 py-1`}>
      {labels[status]}
    </Badge>
  );
}
