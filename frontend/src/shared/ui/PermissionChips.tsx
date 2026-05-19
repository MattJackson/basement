import { Badge } from "@/components/ui/badge";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip";

export interface PermissionChipsProps {
  read: boolean;
  write: boolean;
  owner: boolean;
}

const CHIP_ACTIVE: Record<"R" | "W" | "O", string> = {
  R: "bg-green-50 text-green-700 border-green-200",
  W: "bg-blue-50 text-blue-700 border-blue-200",
  O: "bg-amber-50 text-amber-700 border-amber-200",
};

const CHIP_DESC: Record<"R" | "W" | "O", string> = {
  R: "Read — list and download objects.",
  W: "Write — upload, replace, and delete objects.",
  O: "Owner — full control over the bucket itself (aliases, quotas, deletion).",
};

function Chip({ letter, active }: { letter: "R" | "W" | "O"; active: boolean }) {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        {active ? (
          <Badge
            variant="outline"
            className={`text-[10px] px-1 py-0 h-4 cursor-help ${CHIP_ACTIVE[letter]}`}
          >
            {letter}
          </Badge>
        ) : (
          <span className="text-xs text-muted-foreground opacity-30 cursor-help select-none">
            {letter}
          </span>
        )}
      </TooltipTrigger>
      <TooltipContent>
        <p className="max-w-[18rem] text-xs">
          <strong>{active ? "Granted" : "Not granted"}.</strong>{" "}
          {CHIP_DESC[letter]}
        </p>
      </TooltipContent>
    </Tooltip>
  );
}

/**
 * Three-chip Read/Write/Owner display for bucket-key permission grids.
 * Each chip is keyboard- and screen-reader-accessible via shadcn
 * Tooltip primitives (radix under the hood). Inactive chips render
 * dimmed so the absence is visible without being loud.
 */
export function PermissionChips({ read, write, owner }: PermissionChipsProps) {
  return (
    <TooltipProvider delayDuration={200}>
      <div className="flex items-center gap-1.5">
        <Chip letter="R" active={read} />
        <Chip letter="W" active={write} />
        <Chip letter="O" active={owner} />
      </div>
    </TooltipProvider>
  );
}
