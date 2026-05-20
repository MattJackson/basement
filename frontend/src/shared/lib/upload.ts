// Upload library for drag-drop file uploads with multipart support.
// Handles both single-shot (≤5MB) and multipart (>5MB chunked) uploads.

const CHUNK_SIZE = 5 * 1024 * 1024; // 5 MB chunks for multipart
const RETRYABLE_STATUSES = [500, 502, 503, 504, 429]; // Server errors and rate limiting

export type UploadProgressEvent =
  | { type: "progress"; fileId: string; percent: number }
  | { type: "done"; fileId: string }
  | { type: "error"; fileId: string; error: string }
  | { type: "cancelled"; fileId: string };

export type UploadHooks = {
  onInitMultipart: (cid: string, bid: string, key: string, contentType: string) => Promise<{ uploadId: string }>;
  onPresignPart: (
    cid: string,
    bid: string,
    uploadId: string,
    partNumber: number,
    ttl: number,
  ) => Promise<{ url: string; expires: string }>;
  onCompleteMultipart: (
    cid: string,
    bid: string,
    uploadId: string,
    parts: { partNumber: number; etag: string }[],
  ) => Promise<void>;
  onAbortMultipart: (cid: string, bid: string, uploadId: string) => Promise<void>;
};

export type UploadOptions = {
  file: File;
  hooks: UploadHooks;
  cid: string;
  bid: string;
  key: string;
  contentType?: string;
  ttl?: number; // default 3600s
  onProgress?: (event: UploadProgressEvent) => void;
};

const MULTIPART_THRESHOLD_BYTES = 5 * 1024 * 1024;

export async function uploadFile(options: UploadOptions & { fileId: string }): Promise<void> {
  if (options.file.size <= MULTIPART_THRESHOLD_BYTES) {
    return uploadFileSingleShot(options);
  }
  return uploadFileMultipart(options);
}

async function uploadFileSingleShot(options: UploadOptions & { fileId: string }): Promise<void> {
  // Single-shot path uses direct fetch — `hooks` (the React-Query mutation
  // adapters) is consumed only by the multipart path. Destructure with
  // an alias so the unused-variable lint stays clean.
  const { file, hooks: _hooks, cid, bid, key, contentType = "application/octet-stream", ttl = 3600, onProgress } = options;
  void _hooks;
  const { fileId } = options;

  const emit = (event: UploadProgressEvent) => {
    if (onProgress) onProgress(event);
  };

  try {
    // Get presigned PUT URL
    const response = await fetch(
      `/api/v1/user/clusters/${cid}/buckets/${bid}/objects/${encodeURIComponent(key)}/presign-put?ttl=${ttl}`,
      {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ contentType }),
      }
    );

    if (!response.ok) {
      const errorData = await response.json().catch(() => ({}));
      throw new Error(errorData.error?.message || `Presign PUT failed: ${response.status}`);
    }

    const presign: { url: string; expires: string } = await response.json();

    // Upload file directly to S3 via presigned URL
    emit({ type: "progress", fileId, percent: 0 });
    
    const putResponse = await fetch(presign.url, {
      method: "PUT",
      body: file,
      headers: {
        "Content-Type": contentType,
      },
    });

    if (!putResponse.ok) {
      throw new Error(`Upload failed: ${putResponse.status}`);
    }

    emit({ type: "progress", fileId, percent: 100 });
    emit({ type: "done", fileId });
  } catch (err) {
    const errorMessage = err instanceof Error ? err.message : String(err);
    emit({ type: "error", fileId, error: errorMessage });
    throw err;
  }
}

async function uploadFileMultipart(options: UploadOptions & { fileId: string }): Promise<void> {
  const { file, hooks, cid, bid, key, contentType = "application/octet-stream", ttl = 3600, onProgress } = options;
  const { fileId } = options;

  const emit = (event: UploadProgressEvent) => {
    if (onProgress) onProgress(event);
  };

  const fileSize = file.size;
  
  // Initialize multipart upload
  emit({ type: "progress", fileId, percent: 0 });
  
  let initResponse;
  try {
    initResponse = await hooks.onInitMultipart(cid, bid, key, contentType);
  } catch (err) {
    const errorMessage = err instanceof Error ? err.message : String(err);
    emit({ type: "error", fileId, error: `Failed to initialize multipart upload: ${errorMessage}` });
    throw err;
  }

  const { uploadId } = initResponse;
  let uploadedParts: { partNumber: number; etag: string }[] = [];
  let currentOffset = 0;
  let partNumber = 1;

  try {
    while (currentOffset < fileSize) {
      const chunkSize = Math.min(CHUNK_SIZE, fileSize - currentOffset);
      const chunk = file.slice(currentOffset, currentOffset + chunkSize);

      // Get presigned URL for this part
      let presignResponse;
      try {
        presignResponse = await hooks.onPresignPart(cid, bid, uploadId, partNumber, ttl);
      } catch (err) {
        const errorMessage = err instanceof Error ? err.message : String(err);
        emit({ type: "error", fileId, error: `Failed to presign part ${partNumber}: ${errorMessage}` });
        // Abort multipart on failure
        try {
          await hooks.onAbortMultipart(cid, bid, uploadId);
        } catch {}
        throw err;
      }

      const { url } = presignResponse;

      // Upload the chunk with retry logic for retryable statuses
      let partUploadSuccess = false;
      let retries = 0;
      const maxRetries = 1;

      while (!partUploadSuccess && retries <= maxRetries) {
        const putResponse = await fetch(url, {
          method: "PUT",
          body: chunk,
          headers: {
            "Content-Type": contentType,
          },
        });

        if (RETRYABLE_STATUSES.includes(putResponse.status)) {
          retries++;
          continue;
        }

        if (!putResponse.ok) {
          const errorText = await putResponse.text().catch(() => "");
          throw new Error(`Part ${partNumber} upload failed: ${putResponse.status} ${errorText}`);
        }

        // Extract ETag from response headers (strip quotes)
        const etag = putResponse.headers.get("etag")?.replace(/^"|"$/g, "") || "";
        
        if (!etag) {
          throw new Error(`Part ${partNumber} upload succeeded but no ETag received`);
        }

        uploadedParts.push({ partNumber, etag });
        partUploadSuccess = true;
      }

      if (!partUploadSuccess) {
        const errorMessage = `Part ${partNumber} failed after max retries`;
        emit({ type: "error", fileId, error: errorMessage });
        try {
          await hooks.onAbortMultipart(cid, bid, uploadId);
        } catch {}
        throw new Error(errorMessage);
      }

      currentOffset += chunkSize;
      partNumber++;

      // Emit progress (0-99%)
      const percent = Math.min(99, Math.round((currentOffset / fileSize) * 100));
      emit({ type: "progress", fileId, percent });
    }

    // Complete multipart upload
    await hooks.onCompleteMultipart(cid, bid, uploadId, uploadedParts);
    
    emit({ type: "progress", fileId, percent: 100 });
    emit({ type: "done", fileId });
  } catch (err) {
    // Error already emitted above, rethrow for caller to handle
    throw err;
  }
}

export function cancelUpload(fileId: string, onProgress?: (event: UploadProgressEvent) => void): void {
  if (onProgress) {
    onProgress({ type: "cancelled", fileId });
  }
}
