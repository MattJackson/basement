import { describe, it, expect } from "vitest";
import editSrc from "@/routes/admin/clusters/$cid/edit.tsx?raw";
import newSrc from "@/routes/admin/clusters/new.tsx?raw";

// ADR-0001 (v0.9.0d): the Admin Edit/Add cluster forms must NOT carry
// the per-user Garage S3 credentials (access_key_id / secret_key) any
// longer — those moved to per-user Grants. This test locks that in by
// asserting source-level contracts on the two route files so a future
// edit can't silently re-introduce the fields.

function garageBlock(src: string): string {
  // Lift the body of the Garage branch in handleSave so assertions
  // below ignore AWS S3 + MinIO branches (those legitimately keep
  // access_key + secret_key — different driver model).
  const start = src.indexOf('if (driver === "garage-v1" || driver === "garage") {');
  expect(start, "Garage save branch must exist").toBeGreaterThan(-1);
  const tail = src.slice(start);
  const end = tail.indexOf("} else if (driver ===");
  expect(end, "Garage branch must be followed by an else-if").toBeGreaterThan(-1);
  return tail.slice(0, end);
}

describe("Admin cluster form — ADR-0001 v0.9.0d Garage cred surgery", () => {
  it("Edit page: Garage save branch writes admin_url + admin_token + s3_endpoint, NEVER access_key_id / secret_key", () => {
    const block = garageBlock(editSrc);
    expect(block).toContain("config.admin_url = adminUrl");
    expect(block).toContain("config.admin_token = adminToken");
    expect(block).toContain("if (s3Url) config.s3_endpoint = s3Url");
    expect(block).not.toContain("config.access_key_id");
    expect(block).not.toContain("config.secret_key");
  });

  it("Add page: Garage save branch writes admin_url + admin_token + s3_endpoint, NEVER access_key_id / secret_key", () => {
    const block = garageBlock(newSrc);
    expect(block).toContain("config.admin_url = adminUrl");
    expect(block).toContain("config.admin_token = adminToken");
    expect(block).toContain("if (s3Url) config.s3_endpoint = s3Url");
    expect(block).not.toContain("config.access_key_id");
    expect(block).not.toContain("config.secret_key");
  });

  it("Edit page: Garage S3 plane section no longer renders Access/Secret key inputs", () => {
    const sec = editSrc.indexOf("Garage S3 plane (optional)");
    expect(sec).toBeGreaterThan(-1);
    const sliceEnd = editSrc.indexOf("</section>", sec);
    const section = editSrc.slice(sec, sliceEnd);
    expect(section).not.toContain("s3AccessKey");
    expect(section).not.toContain("s3SecretKey");
    expect(section).not.toContain("Access Key ID");
    expect(section).not.toContain("Secret Access Key");
    expect(section).toContain("Per-user S3 credentials now live as Grants");
  });

  it("Add page: Garage S3 plane section no longer renders Access/Secret key inputs", () => {
    const sec = newSrc.indexOf("Garage S3 plane (optional)");
    expect(sec).toBeGreaterThan(-1);
    const sliceEnd = newSrc.indexOf("</section>", sec);
    const section = newSrc.slice(sec, sliceEnd);
    expect(section).not.toContain("s3AccessKey");
    expect(section).not.toContain("s3SecretKey");
    expect(section).not.toContain("Access Key ID");
    expect(section).not.toContain("Secret Access Key");
    expect(section).toContain("Per-user S3 credentials now live as Grants");
  });

  it("Both pages: garageS3Incomplete tri-state guard is gone (no longer needed without keys field)", () => {
    expect(editSrc).not.toContain("garageS3Incomplete");
    expect(newSrc).not.toContain("garageS3Incomplete");
  });

  it("AWS S3 + MinIO sections retain Access/Secret key wiring (untouched by this cycle)", () => {
    for (const src of [editSrc, newSrc]) {
      expect(src).toContain("awsAccessKey");
      expect(src).toContain("awsSecretKey");
      expect(src).toContain("minioAccessKey");
      expect(src).toContain("minioSecretKey");
      expect(src).toContain("config.access_key = s3AccessKey");
    }
  });
});
