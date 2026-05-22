// Bulk-key parser (v1.3.0d) — used by /files/keys/new bulk-import to
// translate operator-pasted text into a list of region rows ready for
// the POST /api/v1/user/regions/bulk endpoint.
//
// Three input formats are supported, auto-detected from the first
// non-blank line:
//
//   1. aws-cli credentials-file profile blocks:
//        [home]
//        aws_access_key_id = GK...
//        aws_secret_access_key = ...
//        region = us-east-1
//        endpoint_url = https://s3.example.com
//
//   2. CSV with a header row:
//        alias,endpoint,accessKeyId,secretKey,region,addressingStyle
//        home,https://s3.example.com,GK...,abc...,us-east-1,path
//
//   3. TSV (tab-separated) with the same column shape as CSV.
//
// Parsing is best-effort and forgiving — it never throws. Each row
// gets a `rowErrors` array if it can't be turned into a valid request;
// the caller renders the preview table and disables submit when any
// row is invalid.

export interface ParsedKeyRow {
  alias: string;
  endpoint: string;
  accessKeyId: string;
  secretKey: string;
  region: string;
  addressingStyle?: "path" | "virtual_host";
  /** Per-row client-side validation errors. Empty array means valid. */
  rowErrors: string[];
}

export type BulkKeyFormat = "aws-credentials" | "csv" | "tsv" | "unknown";

/**
 * detectFormat sniffs the input and returns one of the three supported
 * formats. The aws-credentials detector wins outright when a `[section]`
 * header is the first non-blank, non-comment line — those blocks are
 * the canonical signal. Otherwise we look at the first non-blank line:
 * a tab anywhere → TSV; a comma → CSV; neither → unknown.
 */
export function detectFormat(input: string): BulkKeyFormat {
  const lines = input
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l.length > 0 && !l.startsWith("#") && !l.startsWith(";"));
  if (lines.length === 0) return "unknown";

  // aws-credentials starts with [profile] header
  if (/^\[[^\]]+\]$/.test(lines[0]!)) {
    return "aws-credentials";
  }

  // TSV beats CSV when a tab is present (an operator who pasted from a
  // spreadsheet column gets TSV by default).
  if (lines[0]!.includes("\t")) return "tsv";
  if (lines[0]!.includes(",")) return "csv";
  return "unknown";
}

/**
 * parseBulkKeys is the single entry point — given an operator-pasted
 * block, returns a list of rows annotated with per-row validation
 * errors. The format is auto-detected; pass an explicit override via
 * the second argument when the test or UI needs to force one path.
 */
export function parseBulkKeys(
  input: string,
  forceFormat?: BulkKeyFormat,
): { format: BulkKeyFormat; rows: ParsedKeyRow[] } {
  const format = forceFormat ?? detectFormat(input);
  switch (format) {
    case "aws-credentials":
      return { format, rows: parseAwsCredentials(input) };
    case "csv":
      return { format, rows: parseDelimited(input, ",") };
    case "tsv":
      return { format, rows: parseDelimited(input, "\t") };
    default:
      return { format, rows: [] };
  }
}

/**
 * parseAwsCredentials interprets the standard aws-cli credentials-file
 * format. Each [profile] block becomes one row; the profile name is
 * the alias. Known keys (case-insensitive):
 *
 *   - aws_access_key_id     → accessKeyId
 *   - aws_secret_access_key → secretKey
 *   - region                → region
 *   - endpoint_url          → endpoint
 *   - addressing_style      → addressingStyle (optional)
 *
 * Unknown keys are silently ignored — the aws-cli file accumulates
 * other settings (output, mfa_serial, ...) and we shouldn't error on
 * them.
 */
function parseAwsCredentials(input: string): ParsedKeyRow[] {
  const out: ParsedKeyRow[] = [];
  const lines = input.split(/\r?\n/);

  let current: Partial<ParsedKeyRow> & { alias?: string } | null = null;

  const flush = () => {
    if (!current) return;
    const row = finalizeRow({
      alias: current.alias ?? "",
      endpoint: current.endpoint ?? "",
      accessKeyId: current.accessKeyId ?? "",
      secretKey: current.secretKey ?? "",
      region: current.region ?? "",
      addressingStyle: current.addressingStyle,
    });
    out.push(row);
    current = null;
  };

  for (const raw of lines) {
    const line = raw.trim();
    if (!line || line.startsWith("#") || line.startsWith(";")) continue;

    const sec = /^\[([^\]]+)\]$/.exec(line);
    if (sec) {
      flush();
      current = { alias: sec[1]!.trim() };
      continue;
    }

    if (!current) continue; // stray key=value before any [section] — skip

    const eq = line.indexOf("=");
    if (eq === -1) continue;
    const key = line.slice(0, eq).trim().toLowerCase();
    const value = line.slice(eq + 1).trim();

    switch (key) {
      case "aws_access_key_id":
        current.accessKeyId = value;
        break;
      case "aws_secret_access_key":
        current.secretKey = value;
        break;
      case "region":
        current.region = value;
        break;
      case "endpoint_url":
      case "endpoint":
        current.endpoint = value;
        break;
      case "addressing_style":
      case "addressingstyle":
        if (value === "path" || value === "virtual_host") {
          current.addressingStyle = value;
        }
        break;
      default:
        // Unknown key — silently ignore.
        break;
    }
  }
  flush();
  return out;
}

/**
 * parseDelimited handles both CSV and TSV. The first non-blank line is
 * treated as the header row and used to map columns to row fields.
 * Recognised header names (case-insensitive, with snake_case + camelCase
 * variants both accepted):
 *
 *   - alias
 *   - endpoint
 *   - accesskeyid | access_key_id
 *   - secretkey   | secret_key | secret_access_key
 *   - region
 *   - addressingstyle | addressing_style
 *
 * Each subsequent non-blank line is split on the delimiter and the
 * positional values are assigned by header. Missing columns leave the
 * field empty (caller surfaces the validation error).
 *
 * Quoted values ("foo,bar") are NOT supported — operators pasting
 * comma-containing secrets is an unusual edge case and parsing quoted
 * CSV blows up the surface area. Document the limitation in the UI.
 */
function parseDelimited(input: string, delimiter: string): ParsedKeyRow[] {
  const lines = input
    .split(/\r?\n/)
    .map((l) => l.trim())
    .filter((l) => l.length > 0 && !l.startsWith("#"));

  if (lines.length < 2) return []; // need header + at least one row

  const header = lines[0]!.split(delimiter).map((c) => normalizeHeader(c.trim()));

  const rows: ParsedKeyRow[] = [];
  for (let i = 1; i < lines.length; i++) {
    const cells = lines[i]!.split(delimiter).map((c) => c.trim());
    const row: Partial<ParsedKeyRow> = {};
    for (let j = 0; j < header.length; j++) {
      const key = header[j];
      const val = cells[j] ?? "";
      if (!key) continue;
      switch (key) {
        case "alias":
          row.alias = val;
          break;
        case "endpoint":
          row.endpoint = val;
          break;
        case "accessKeyId":
          row.accessKeyId = val;
          break;
        case "secretKey":
          row.secretKey = val;
          break;
        case "region":
          row.region = val;
          break;
        case "addressingStyle":
          if (val === "path" || val === "virtual_host") {
            row.addressingStyle = val;
          }
          break;
      }
    }
    rows.push(
      finalizeRow({
        alias: row.alias ?? "",
        endpoint: row.endpoint ?? "",
        accessKeyId: row.accessKeyId ?? "",
        secretKey: row.secretKey ?? "",
        region: row.region ?? "",
        addressingStyle: row.addressingStyle,
      }),
    );
  }
  return rows;
}

function normalizeHeader(h: string): keyof ParsedKeyRow | "" {
  const lower = h.toLowerCase().replace(/[_-]/g, "");
  switch (lower) {
    case "alias":
    case "name":
    case "profile":
      return "alias";
    case "endpoint":
    case "endpointurl":
    case "url":
      return "endpoint";
    case "accesskeyid":
    case "awsaccesskeyid":
    case "accesskey":
      return "accessKeyId";
    case "secretkey":
    case "secretaccesskey":
    case "awssecretaccesskey":
      return "secretKey";
    case "region":
      return "region";
    case "addressingstyle":
      return "addressingStyle";
    default:
      return "";
  }
}

function finalizeRow(input: Omit<ParsedKeyRow, "rowErrors">): ParsedKeyRow {
  const errors: string[] = [];
  if (!input.alias) errors.push("alias is required");
  if (!input.endpoint) errors.push("endpoint is required");
  if (!input.accessKeyId) errors.push("accessKeyId is required");
  if (!input.secretKey) errors.push("secretKey is required");

  // Light URL validation — matches the server-side check.
  if (input.endpoint) {
    try {
      const u = new URL(input.endpoint);
      if (!u.protocol || !u.host) {
        errors.push("endpoint must be a full URL with scheme and host");
      }
    } catch {
      errors.push("endpoint must be a full URL with scheme and host");
    }
  }

  return {
    alias: input.alias,
    endpoint: input.endpoint,
    accessKeyId: input.accessKeyId,
    secretKey: input.secretKey,
    region: input.region || "us-east-1",
    addressingStyle: input.addressingStyle,
    rowErrors: errors,
  };
}
