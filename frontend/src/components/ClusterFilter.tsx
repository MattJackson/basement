import { useListClusters } from "@/shared/api/queries";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";

interface ClusterFilterProps {
  selectedClusterId: string | null;
  onFilterChange: (clusterId: string | null) => void;
}

export function ClusterFilter({ selectedClusterId, onFilterChange }: ClusterFilterProps) {
  const { data: connections, isLoading } = useListClusters();

  const [open, setOpen] = useState(false);

  const options = useMemo(() => {
    return (connections ?? []).sort((a, b) => a.label.localeCompare(b.label));
  }, [connections]);

  const selectedLabel =
    selectedClusterId === null
      ? "All clusters"
      : connections?.find((c) => c.id === selectedClusterId)?.label ?? selectedClusterId;

  if (isLoading) {
    return <Button variant="outline" size="sm" disabled>Loading...</Button>;
  }

  return (
    <DropdownMenu open={open} onOpenChange={setOpen}>
      <DropdownMenuTrigger>
        <Button variant="outline" size="sm">
          {selectedLabel}
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" className="w-56">
        <DropdownMenuItem
          onClick={() => {
            onFilterChange(null);
            setOpen(false);
          }}
          className="cursor-pointer"
        >
          All clusters
        </DropdownMenuItem>
        {options.map((conn) => (
          <DropdownMenuItem
            key={conn.id}
            onClick={() => {
              onFilterChange(conn.id);
              setOpen(false);
            }}
            className="cursor-pointer flex items-center gap-2"
          >
            <span
              className="h-2 w-2 rounded-full"
              style={{ backgroundColor: conn.color ?? "#C9874B" }}
              aria-hidden="true"
            />
            {conn.label}
          </DropdownMenuItem>
        ))}
      </DropdownMenuContent>
    </DropdownMenu>
  );
}
