import { useState, useCallback, useRef } from "react";
import { X, Upload as UploadIcon, File as FileIcon, CheckCircle2, AlertCircle, Loader2 } from "lucide-react";
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { API_BASE } from "@/shared/api/client";

type FileUpload = {
  file: File;
  fileId: string;
  key: string;
  status: "pending" | "uploading" | "done" | "error" | "cancelled";
  progress: number;
  error?: string;
};

type UploadDialogProps = {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  // Region keychain id (ADR-0002). The legacy cluster-tier cid path
  // retired in v1.1.0e along with the /user/clusters/* endpoints.
  regionId: string;
  bid: string;
  prefix: string; // destination folder/prefix
  onSuccess?: () => void;
};

// uploadBaseFor builds the /user/regions/{regionId}/buckets/{bid} prefix
// used by every signed-URL request out of this dialog.
function uploadBaseFor(opts: { regionId: string; bid: string }): string {
  return `${API_BASE}/user/regions/${encodeURIComponent(opts.regionId)}/buckets/${encodeURIComponent(opts.bid)}`;
}

// Per-part retry budget on a retryable (5xx/429) PUT status. Each retry
// re-presigns the part URL — presigned URLs can be single-use, so reusing
// the same URL across attempts is unsafe.
const PART_MAX_RETRIES = 1;

// Simple upload implementation inline to avoid complex hook state management.
// `base` is the per-bucket URL prefix — /api/v1/user/regions/{regionId}/buckets/{bid}.
async function doUpload(
  file: File,
  base: string,
  key: string,
  contentType: string,
  onProgress: (event: { type: "progress" | "done" | "error"; percent?: number; error?: string }) => void,
  uploadId: string | null = null
): Promise<void> {
  const CHUNK_SIZE = 5 * 1024 * 1024; // 5 MB
  const RETRYABLE_STATUSES = [500, 502, 503, 504, 429];

  try {
    // Single-shot upload for files <= 5 MB
    if (file.size <= CHUNK_SIZE && !uploadId) {
      const presignRes = await fetch(
        `${base}/objects/${encodeURIComponent(key)}/presign-put?ttl=3600`,
        {
          method: "POST",
          credentials: "include",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ contentType }),
        }
      );

      if (!presignRes.ok) {
        const err = await presignRes.json().catch(() => ({}));
        throw new Error(err.error?.message || `Presign failed: ${presignRes.status}`);
      }

      const presign: { url: string } = await presignRes.json();

      onProgress({ type: "progress", percent: 0 });

      const putRes = await fetch(presign.url, {
        method: "PUT",
        body: file,
        headers: { "Content-Type": contentType },
      });

      if (!putRes.ok) {
        throw new Error(`Upload failed: ${putRes.status}`);
      }

      onProgress({ type: "progress", percent: 100 });
      onProgress({ type: "done" });
      return;
    }

    // Multipart upload for files > 5 MB or if uploadId provided
    if (!uploadId) {
      const initRes = await fetch(`${base}/multipart/init`, {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ key, contentType }),
      });

      if (!initRes.ok) {
        const err = await initRes.json().catch(() => ({}));
        throw new Error(err.error?.message || `Init failed: ${initRes.status}`);
      }

      const initData: { uploadId: string } = await initRes.json();
      uploadId = initData.uploadId;
    }

    // Upload parts
    let currentOffset = 0;
    let partNumber = 1;
    const fileSize = file.size;
    const uploadedParts: { partNumber: number; etag: string }[] = [];

    while (currentOffset < fileSize) {
      const chunkSize = Math.min(CHUNK_SIZE, fileSize - currentOffset);
      const chunk = file.slice(currentOffset, currentOffset + chunkSize);

      // Upload chunk with retry logic. Each attempt re-presigns the part:
      // presigned URLs can be single-use, so reusing one across a retry can
      // fail or upload nothing. Re-presigning per attempt keeps the retry
      // honest.
      let retries = 0;
      let partSuccess = false;

      while (!partSuccess && retries <= PART_MAX_RETRIES) {
        // Get a fresh presigned URL for this part on every attempt.
        const presignRes = await fetch(
          `${base}/multipart/${encodeURIComponent(uploadId)}/part/${partNumber}/presign?ttl=3600`,
          {
            method: "POST",
            credentials: "include",
          }
        );

        if (!presignRes.ok) {
          const err = await presignRes.json().catch(() => ({}));
          throw new Error(err.error?.message || `Part ${partNumber} presign failed: ${presignRes.status}`);
        }

        const presign: { url: string; expires: string } = await presignRes.json();

        const putRes = await fetch(presign.url, {
          method: "PUT",
          body: chunk,
          headers: { "Content-Type": contentType },
        });

        if (RETRYABLE_STATUSES.includes(putRes.status)) {
          retries++;
          continue;
        }

        if (!putRes.ok) {
          const errText = await putRes.text().catch(() => "");
          throw new Error(`Part ${partNumber} failed: ${putRes.status} ${errText}`);
        }

        const etag = putRes.headers.get("etag")?.replace(/^"|"$/g, "") || "";

        if (!etag) {
          throw new Error(`Part ${partNumber} succeeded but no ETag`);
        }

        uploadedParts.push({ partNumber, etag });
        partSuccess = true;
      }

      if (!partSuccess) {
        // Abort multipart on failure. v1.11.0.6: the abort handler now
        // requires the object key (S3's AbortMultipartUpload needs it)
        // — pass it as ?key= query param per BUG04 fix.
        await fetch(
          `${base}/multipart/${encodeURIComponent(uploadId)}?key=${encodeURIComponent(key)}`,
          {
            method: "DELETE",
            credentials: "include",
          },
        ).catch(() => {});

        throw new Error(`Part ${partNumber} failed after retries`);
      }

      currentOffset += chunkSize;
      partNumber++;

      const percent = Math.min(99, Math.round((currentOffset / fileSize) * 100));
      onProgress({ type: "progress", percent });
    }

    // Complete multipart upload
    const completeRes = await fetch(
      `${base}/multipart/${encodeURIComponent(uploadId)}/complete`,
      {
        method: "POST",
        credentials: "include",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ parts: uploadedParts }),
      }
    );

    if (!completeRes.ok) {
      const err = await completeRes.json().catch(() => ({}));
      throw new Error(err.error?.message || `Complete failed: ${completeRes.status}`);
    }

    onProgress({ type: "progress", percent: 100 });
    onProgress({ type: "done" });
  } catch (err) {
    const errorMessage = err instanceof Error ? err.message : String(err);
    onProgress({ type: "error", error: errorMessage });
    throw err;
  }
}

export function UploadDialog({ open, onOpenChange, regionId, bid, prefix, onSuccess }: UploadDialogProps) {
  const base = uploadBaseFor({ regionId, bid });
  const [files, setFiles] = useState<FileUpload[]>([]);
  const [isDragging, setIsDragging] = useState(false);
  const dragCounter = useRef(0);

  // filesRef mirrors `files` so the upload-orchestration callbacks
  // (startUploads / retryUpload / retryFailed) can read the CURRENT row
  // set synchronously. Reading the `files` state variable directly would
  // capture a stale render snapshot (the bug the audit flagged: allDone /
  // retry computed against pre-upload state). We keep it in lockstep by
  // assigning on each render.
  const filesRef = useRef(files);
  filesRef.current = files;

  const generateFileKey = (file: File): string => {
    return `${prefix}${file.name}`.replace(/\/+/g, "/");
  };

  const handleFiles = useCallback((newFiles: File[]) => {
    setFiles(prev => [
      ...prev,
      ...newFiles.map<FileUpload>(file => ({
        file,
        fileId: `${file.name}-${Date.now()}-${Math.random().toString(36).substr(2, 9)}`,
        key: generateFileKey(file),
        status: "pending" as const,
        progress: 0,
      })),
    ]);
  }, [prefix]);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    dragCounter.current++;
    setIsDragging(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    dragCounter.current--;
    if (dragCounter.current === 0) {
      setIsDragging(false);
    }
  }, []);

  const handleDrop = useCallback((e: React.DragEvent) => {
    e.preventDefault();
    dragCounter.current = 0;
    setIsDragging(false);
    
    const droppedFiles = Array.from(e.dataTransfer.files);
    if (droppedFiles.length > 0) {
      handleFiles(droppedFiles);
    }
  }, [handleFiles]);

  const handleFileInput = useCallback((e: React.ChangeEvent<HTMLInputElement>) => {
    const selectedFiles = Array.from(e.target.files || []);
    if (selectedFiles.length > 0) {
      handleFiles(selectedFiles);
    }
    e.target.value = "";
  }, [handleFiles]);

  // uploadOneFile runs a single upload and resolves to true on success,
  // false on failure. It marks the row "uploading" up front and tracks
  // progress/done/error via the onProgress callback. Returning a boolean
  // (instead of relying on a post-await read of `files`, which would be a
  // stale closure) lets the caller compute completion from the resolved
  // promise results directly.
  const uploadOneFile = useCallback(async (upload: FileUpload): Promise<boolean> => {
    setFiles(prev => prev.map(f =>
      f.fileId === upload.fileId ? { ...f, status: "uploading", progress: 0, error: undefined } : f
    ));
    try {
      await doUpload(
        upload.file,
        base,
        upload.key,
        upload.file.type,
        (event) => {
          setFiles(prev => prev.map(f =>
            f.fileId === upload.fileId ? {
              ...f,
              progress: event.type === "progress" ? (event.percent ?? 0) : f.progress,
              status: event.type === "done" ? "done" : event.type === "error" ? "error" : f.status,
              error: event.error ?? f.error,
            } : f
          ));
        }
      );
      return true;
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setFiles(prev => prev.map(f =>
        f.fileId === upload.fileId ? { ...f, status: "error", error: message } : f
      ));
      return false;
    }
  }, [base]);

  const cancelUpload = useCallback((fileId: string) => {
    setFiles(prev => prev.map(f =>
      f.fileId === fileId ? { ...f, status: "cancelled", progress: 0 } : f
    ));
  }, []);

  // retryUpload re-runs a single failed/cancelled upload. We resolve the
  // target row from the functional setFiles updater (current state) rather
  // than a closed-over `files` snapshot, then kick the upload directly with
  // the known FileUpload object — no setTimeout round-trip.
  const retryUpload = useCallback((fileId: string) => {
    const target = filesRef.current.find(f => f.fileId === fileId);
    if (target && (target.status === "error" || target.status === "cancelled")) {
      void uploadOneFile(target);
    }
  }, [uploadOneFile]);

  const cancelAll = useCallback(() => {
    setFiles(prev => prev.map(f =>
      f.status === "pending" || f.status === "uploading" ? { ...f, status: "cancelled", progress: 0 } : f
    ));
  }, []);

  // retryFailed re-runs every errored row. Reads current state via the
  // functional updater so it captures the live `files`, not a stale closure.
  const retryFailed = useCallback(() => {
    const failed = filesRef.current.filter(f => f.status === "error");
    for (const upload of failed) {
      void uploadOneFile(upload);
    }
  }, [uploadOneFile]);

  const startUploads = useCallback(async () => {
    const pendingFiles = filesRef.current.filter(f => f.status === "pending");
    if (pendingFiles.length === 0) return;

    // Upload all pending files in parallel. uploadOneFile resolves to a
    // boolean; completion is computed from the resolved results, not from a
    // stale post-await read of `files`.
    const results = await Promise.all(pendingFiles.map(upload => uploadOneFile(upload)));

    if (results.every(Boolean) && onSuccess) {
      onSuccess();
    }
  }, [uploadOneFile, onSuccess]);

  const hasPendingOrUploading = files.some(f => f.status === "pending" || f.status === "uploading");
  const hasError = files.some(f => f.status === "error");
  const allDone = files.length > 0 && files.every(f => f.status === "done" || f.status === "cancelled");

  return (
    <Dialog open={open} onOpenChange={(isOpen) => {
      if (!isOpen && hasPendingOrUploading) return;
      onOpenChange(isOpen);
    }}>
      <DialogContent className="sm:max-w-[600px]">
        <DialogHeader>
          <DialogTitle>Upload Files</DialogTitle>
        </DialogHeader>

        {/* Drag & Drop Zone */}
        <div
          onDragOver={handleDragOver}
          onDragLeave={handleDragLeave}
          onDrop={handleDrop}
          className={`border-2 border-dashed rounded-lg p-8 text-center transition-colors ${
            isDragging ? "border-blue-500 bg-blue-50" : "border-gray-300 hover:border-gray-400"
          }`}
        >
          <UploadIcon className="w-12 h-12 mx-auto text-gray-400 mb-4" />
          <p className="text-sm text-gray-600 mb-2">
            Drag and drop files here, or click to select
          </p>
          <input
            type="file"
            multiple
            onChange={handleFileInput}
            className="hidden"
            id="file-input"
          />
          <label htmlFor="file-input">
            <Button type="button" variant="outline">
              Select Files
            </Button>
          </label>
        </div>

        {/* File List */}
        {files.length > 0 && (
          <div className="space-y-3 mt-4 max-h-96 overflow-y-auto">
            {files.map((upload) => (
              <Card key={upload.fileId}>
                <CardContent className="p-4">
                  <div className="flex items-start gap-3">
                    <FileIcon className={`w-8 h-8 mt-1 ${
                      upload.status === "done" ? "text-green-500" :
                      upload.status === "error" ? "text-red-500" :
                      upload.status === "cancelled" ? "text-gray-400" :
                      "text-blue-500"
                    }`} />
                    
                    <div className="flex-1 min-w-0">
                      <p className="font-medium text-sm truncate">{upload.file.name}</p>
                      <p className="text-xs text-gray-500">
                        {(upload.file.size / 1024).toFixed(1)} KB • {upload.key}
                      </p>

                      {/* Status Indicators */}
                      <div className="flex items-center gap-2 mt-1">
                        {upload.status === "done" && (
                          <>
                            <CheckCircle2 className="w-4 h-4 text-green-500" />
                            <span className="text-xs text-green-600">Done</span>
                          </>
                        )}
                        {upload.status === "error" && (
                          <>
                            <AlertCircle className="w-4 h-4 text-red-500" />
                            <span className="text-xs text-red-600 truncate">{upload.error}</span>
                          </>
                        )}
                        {upload.status === "cancelled" && (
                          <span className="text-xs text-gray-500">Cancelled</span>
                        )}
                        {upload.status === "uploading" && (
                          <>
                            <Loader2 className="w-4 h-4 text-blue-500 animate-spin" />
                            <span className="text-xs text-blue-600">{upload.progress}% uploaded</span>
                          </>
                        )}
                      </div>
                    </div>

                    {/* Action Buttons */}
                    <div className="flex items-center gap-1">
                      {upload.status === "error" && (
                        <>
                          <Button size="sm" variant="outline" onClick={() => retryUpload(upload.fileId)}>
                            Retry
                          </Button>
                          <Button 
                            size="sm" 
                            variant="ghost" 
                            onClick={() => cancelUpload(upload.fileId)}
                          >
                            <X className="w-4 h-4" />
                          </Button>
                        </>
                      )}
                      {(upload.status === "pending" || upload.status === "uploading") && (
                        <Button size="sm" variant="ghost" onClick={() => cancelUpload(upload.fileId)}>
                          Cancel
                        </Button>
                      )}
                    </div>
                  </div>
                </CardContent>
              </Card>
            ))}
          </div>
        )}

        {/* Footer Actions */}
        <div className="flex justify-between items-center pt-4 border-t">
          <Button 
            variant="ghost" 
            size="sm" 
            onClick={cancelAll}
            disabled={!hasPendingOrUploading || allDone}
          >
            Cancel All
          </Button>

          <div className="flex gap-2">
            {hasError && (
              <Button
                variant="outline"
                size="sm"
                onClick={retryFailed}
              >
                Retry Failed
              </Button>
            )}
            
            {!allDone && (
              <Button 
                size="sm" 
                onClick={startUploads}
                disabled={!hasPendingOrUploading}
              >
                Upload {files.filter(f => f.status === "pending").length} Files
              </Button>
            )}

            {allDone && (
              <Button size="sm" onClick={() => onOpenChange(false)}>
                Close
              </Button>
            )}
          </div>
        </div>
      </DialogContent>
    </Dialog>
  );
}
