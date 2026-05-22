// Tests for the bulk-key parser (v1.3.0d). Covers all three accepted
// formats — aws-cli credentials profile, CSV, TSV — plus the per-row
// validation surface.

import { describe, it, expect } from "vitest";
import { detectFormat, parseBulkKeys } from "./bulkKeyParse";

describe("bulkKeyParse — format detection", () => {
  it("detects aws-credentials by leading [section] header", () => {
    expect(detectFormat("[home]\naws_access_key_id = X")).toBe("aws-credentials");
  });

  it("detects TSV when the header has tabs", () => {
    expect(detectFormat("alias\tendpoint\taccessKeyId\tsecretKey")).toBe("tsv");
  });

  it("detects CSV when the header has commas (no tabs)", () => {
    expect(detectFormat("alias,endpoint,accessKeyId,secretKey")).toBe("csv");
  });

  it("returns unknown for empty or garbage input", () => {
    expect(detectFormat("")).toBe("unknown");
    expect(detectFormat("just some text")).toBe("unknown");
  });

  it("ignores leading comment lines when detecting", () => {
    const input = "# a comment\n; another\n[work]\naws_access_key_id=X";
    expect(detectFormat(input)).toBe("aws-credentials");
  });
});

describe("bulkKeyParse — aws-credentials format", () => {
  it("parses one profile block end-to-end", () => {
    const input = `
[home]
aws_access_key_id = GKHOME
aws_secret_access_key = secret-home
region = us-east-1
endpoint_url = https://s3.home.example.com
`;
    const { format, rows } = parseBulkKeys(input);
    expect(format).toBe("aws-credentials");
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({
      alias: "home",
      endpoint: "https://s3.home.example.com",
      accessKeyId: "GKHOME",
      secretKey: "secret-home",
      region: "us-east-1",
      rowErrors: [],
    });
  });

  it("parses multiple profiles into multiple rows", () => {
    const input = `
[work]
aws_access_key_id = GK1
aws_secret_access_key = s1
endpoint_url = https://s3.work.example.com

[home]
aws_access_key_id = GK2
aws_secret_access_key = s2
endpoint_url = https://s3.home.example.com
`;
    const { rows } = parseBulkKeys(input);
    expect(rows).toHaveLength(2);
    expect(rows.map((r) => r.alias)).toEqual(["work", "home"]);
  });

  it("ignores unknown keys silently", () => {
    const input = `
[main]
aws_access_key_id = GKX
aws_secret_access_key = sx
endpoint_url = https://s3.example.com
output = json
mfa_serial = arn:aws:iam::123:mfa/me
`;
    const { rows } = parseBulkKeys(input);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.rowErrors).toEqual([]);
  });

  it("flags a profile missing required fields", () => {
    const input = `
[incomplete]
aws_access_key_id = GK
`;
    const { rows } = parseBulkKeys(input);
    expect(rows).toHaveLength(1);
    expect(rows[0]!.rowErrors.length).toBeGreaterThan(0);
  });

  it("accepts optional addressing_style", () => {
    const input = `
[virt]
aws_access_key_id = GK
aws_secret_access_key = s
endpoint_url = https://s3.example.com
addressing_style = virtual_host
`;
    const { rows } = parseBulkKeys(input);
    expect(rows[0]!.addressingStyle).toBe("virtual_host");
  });
});

describe("bulkKeyParse — CSV format", () => {
  it("parses a CSV with the canonical header", () => {
    const input = `alias,endpoint,accessKeyId,secretKey,region
home,https://s3.home.example.com,GKH,secH,us-east-1
work,https://s3.work.example.com,GKW,secW,us-east-1`;
    const { format, rows } = parseBulkKeys(input);
    expect(format).toBe("csv");
    expect(rows).toHaveLength(2);
    expect(rows[0]!).toMatchObject({
      alias: "home",
      endpoint: "https://s3.home.example.com",
      accessKeyId: "GKH",
      secretKey: "secH",
      region: "us-east-1",
      rowErrors: [],
    });
  });

  it("accepts snake_case header variants", () => {
    const input = `alias,endpoint,access_key_id,secret_access_key,region
home,https://s3.example.com,GK,sec,us-east-1`;
    const { rows } = parseBulkKeys(input);
    expect(rows[0]!).toMatchObject({
      accessKeyId: "GK",
      secretKey: "sec",
    });
  });

  it("flags malformed endpoint URL", () => {
    const input = `alias,endpoint,accessKeyId,secretKey
broken,not-a-url,GK,sec`;
    const { rows } = parseBulkKeys(input);
    expect(rows[0]!.rowErrors.length).toBeGreaterThan(0);
  });

  it("flags missing required column values", () => {
    const input = `alias,endpoint,accessKeyId,secretKey
,https://s3.example.com,GK,sec`;
    const { rows } = parseBulkKeys(input);
    expect(rows[0]!.rowErrors).toEqual(expect.arrayContaining(["alias is required"]));
  });

  it("defaults region to us-east-1 when omitted", () => {
    const input = `alias,endpoint,accessKeyId,secretKey
home,https://s3.example.com,GK,sec`;
    const { rows } = parseBulkKeys(input);
    expect(rows[0]!.region).toBe("us-east-1");
  });

  it("accepts addressingStyle column when present", () => {
    const input = `alias,endpoint,accessKeyId,secretKey,addressingStyle
aws,https://s3.us-east-1.amazonaws.com,GK,sec,virtual_host`;
    const { rows } = parseBulkKeys(input);
    expect(rows[0]!.addressingStyle).toBe("virtual_host");
  });
});

describe("bulkKeyParse — TSV format", () => {
  it("parses a TSV with the canonical header", () => {
    const input = `alias\tendpoint\taccessKeyId\tsecretKey\tregion
home\thttps://s3.home.example.com\tGKH\tsecH\tus-east-1
work\thttps://s3.work.example.com\tGKW\tsecW\tus-east-1`;
    const { format, rows } = parseBulkKeys(input);
    expect(format).toBe("tsv");
    expect(rows).toHaveLength(2);
    expect(rows[1]!).toMatchObject({
      alias: "work",
      endpoint: "https://s3.work.example.com",
      accessKeyId: "GKW",
      secretKey: "secW",
    });
  });
});

describe("bulkKeyParse — empty / pathological input", () => {
  it("returns no rows for empty input", () => {
    const { rows } = parseBulkKeys("");
    expect(rows).toEqual([]);
  });

  it("returns no rows for CSV with only a header", () => {
    const { rows } = parseBulkKeys("alias,endpoint,accessKeyId,secretKey");
    expect(rows).toEqual([]);
  });
});
