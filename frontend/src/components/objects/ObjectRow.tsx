import { Button } from "@/components/ui/button";
import { TableCell } from "@/components/ui/table";
import type { components } from "@/shared/api/types.gen";
import { humanizeBytes, humanizeTime } from "@/shared/lib/format";

type ObjectRowProps = {
  object: components["schemas"]["ObjectInfo"];
  isFolder?: boolean;
  onFolderClick?: (prefix: string) => void;
  onDownload?: (key: string) => void;
};

export function ObjectRow({ object, isFolder = false, onFolderClick, onDownload }: ObjectRowProps) {
  // Folder prefixes from S3 end in "/" (e.g. "raw/"). Stripping the
  // trailing slash before pop() yields "raw" instead of an empty string.
  const trimmed = isFolder && object.key.endsWith("/") ? object.key.slice(0, -1) : object.key;
  const displayName = trimmed.split("/").pop() || trimmed;

  if (isFolder) {
    return (
      <tr className="cursor-pointer hover:bg-muted/50" onClick={() => onFolderClick?.(object.key)}>
        <TableCell>
          <div className="flex items-center gap-2">
            <svg
              xmlns="http://www.w3.org/2000/svg"
              viewBox="0 0 24 24"
              fill="currentColor"
              className="h-5 w-5 text-blue-500"
            >
              <path d="M19.5 21a3 3 0 0 0 3-3v-4.5a3 3 0 0 0-3-3h-15a3 3 0 0 0-3 3V18a3 3 0 0 0 3 3h15ZM1.5 10.146V6a3 3 0 0 1 3-3h5.379a2.25 2.25 0 0 1 1.59.659l2.122 2.121c.14.141.331.22.53.22H19.5a3 3 0 0 1 3 3v1.146A4.483 4.483 0 0 0 19.5 9h-15a4.483 4.483 0 0 0-3 1.146Z" />
            </svg>
            <span className="font-medium">{displayName}</span>
          </div>
        </TableCell>
        <TableCell className="text-right">—</TableCell>
        <TableCell className="text-right">—</TableCell>
        <TableCell></TableCell>
      </tr>
    );
  }

  return (
    <tr>
      <TableCell>
        <div className="flex items-center gap-2">
          <svg
            xmlns="http://www.w3.org/2000/svg"
            viewBox="0 0 24 24"
            fill="currentColor"
            className="h-5 w-5 text-gray-500"
          >
            <path d="M19.5 14.25v-9a3 3 0 0 0-3-3H7.5a3 3 0 0 0-3 3v13.5a3 3 0 0 0 3 3h9a3 3 0 0 0 3-3Z" />
            <path d="M16.5 7.5h-9v9h9a1.5 1.5 0 0 0 1.5-1.5v-6Z" fillOpacity={0} stroke="currentColor" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span className="font-medium">{displayName}</span>
        </div>
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {humanizeBytes(object.size)}
      </TableCell>
      <TableCell className="text-right tabular-nums">
        {object.last_modified ? humanizeTime(object.last_modified) : "—"}
      </TableCell>
      <TableCell>
        <div className="min-h-[44px] flex items-center justify-end">
          <Button
            variant="ghost"
            size="sm"
            onClick={(e) => {
              e.stopPropagation();
              onDownload?.(object.key);
            }}
          >
            Download
          </Button>
        </div>
      </TableCell>
    </tr>
  );
}
