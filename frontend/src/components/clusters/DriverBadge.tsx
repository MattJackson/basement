import { Badge } from "@/components/ui/badge";

interface DriverBadgeProps {
  driver: string;
}

const DRIVER_LABELS: Record<string, string> = {
  "garage-v1": "Garage v1",
  "garage": "Garage",
  "aws-s3": "AWS S3",
  "minio": "MinIO",
};

export function DriverBadge({ driver }: DriverBadgeProps) {
  const label = DRIVER_LABELS[driver] ?? driver;
  
  return (
    <Badge variant="secondary" className="font-normal">
      {label}
    </Badge>
  );
}
