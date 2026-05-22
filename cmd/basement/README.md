# basement CLI

`basement` is the official command-line client for [basement](../../README.md).
It talks to a running basement deployment using **service-account
bearer credentials** (v1.7.0a), so it works without browser cookies and
is suitable for CI runners, scripts, and operator one-liners.

The CLI ships in `v1.8.0a` and tracks the server's API surface.

## Install

### From source (current state)

```sh
go install github.com/mattjackson/basement/cmd/basement@latest
```

Or from a clone:

```sh
git clone https://github.com/mattjackson/basement
cd basement
go build ./cmd/basement/...
# Binary lands at ./basement — drop it on your $PATH.
```

### Cross-compile a release artifact (manual)

```sh
# darwin/arm64
GOOS=darwin  GOARCH=arm64 go build -o basement-darwin-arm64  ./cmd/basement/...
# darwin/amd64
GOOS=darwin  GOARCH=amd64 go build -o basement-darwin-amd64  ./cmd/basement/...
# linux/amd64
GOOS=linux   GOARCH=amd64 go build -o basement-linux-amd64   ./cmd/basement/...
# linux/arm64
GOOS=linux   GOARCH=arm64 go build -o basement-linux-arm64   ./cmd/basement/...
# windows/amd64
GOOS=windows GOARCH=amd64 go build -o basement-windows-amd64.exe ./cmd/basement/...
```

CI cross-compile + release upload is intentionally deferred this
cycle — the existing `release.yml` is Docker-only and adding multi-arch
artifact upload is out of scope for v1.8.0a.

## Authentication

The CLI uses **service-account bearer auth** (see
`internal/auth/bearer.go`). Mint a service account first:

1. Sign into basement as a `host:manage_users` admin.
2. Open **Admin → Service Accounts** and click **New service account**.
3. Pick a name, grant the capabilities the CLI will need
   (e.g. `host:manage_users` for `keys *` commands; nothing extra for
   region / bucket / object ops because those are gated by the
   user-tier owner check), and create.
4. The plaintext secret appears **once**. Copy it.

Then on your workstation:

```sh
basement login \
  --endpoint https://basement.example.com \
  --key BMNT0000111122223333 \
  --secret <plaintext>
```

This writes `~/.config/basement/config.yaml` (mode 0600).

### Profiles

Multiple deployments are supported via named profiles. `basement login`
respects `--profile NAME` (or `$BASEMENT_PROFILE`) so you can hold
`default`, `staging`, `prod`, … side-by-side.

```sh
basement --profile staging regions list
BASEMENT_PROFILE=prod basement keys list
```

### CI usage

For automation, set `$BASEMENT_SECRET_KEY` in your CI env instead of
committing the secret to disk:

```sh
export BASEMENT_SECRET_KEY=...
basement --profile ci regions list
```

The endpoint + access key still come from the on-disk profile so audit
records can pin the runner to a known credential.

## Commands

```
basement login --endpoint URL --key BMNT... --secret ...
basement regions list
basement regions add ALIAS ENDPOINT KEY SECRET REGION
basement regions delete REGION_ID
basement buckets list [--region REGION_ID]
basement objects list BUCKET [--prefix PFX] [--region REGION_ID]
basement objects get BUCKET KEY [--output FILE]      # presign → streamed download
basement objects put BUCKET KEY FILE                  # presign → streamed upload
basement objects delete BUCKET KEY
basement keys list
basement keys create NAME --capability foo --scope host:*
basement keys rotate ID
basement keys delete ID
basement version
basement --help
```

### Global flags

- `--profile NAME` — config-file profile (defaults to `default` or
  `$BASEMENT_PROFILE`).
- `--region REGION_ID` — override the profile's current region for one
  invocation.
- `--output-format json|table` — switch the renderer. `table` is the
  default; `json` is machine-readable and indented for piping to `jq`.

## Examples

List regions across two deployments:

```sh
basement --profile staging regions list
basement --profile prod    regions list
```

Add a region for an S3-compatible backend:

```sh
basement regions add home https://s3.us-east-1.amazonaws.com \
  AKIAEXAMPLE EXAMPLESECRET us-east-1
```

Browse a bucket and pipe to `jq`:

```sh
basement --output-format json objects list mybucket --prefix photos/ |
  jq '.objects[].key'
```

Download an object directly via a presigned URL (basement never
proxies the bytes):

```sh
basement objects get mybucket photos/2026-04/cat.jpg --output /tmp/cat.jpg
```

Upload a file:

```sh
basement objects put mybucket data/report.parquet ./report.parquet
```

Mint a new service account for a CI runner:

```sh
basement keys create ci-runner \
  --capability host:manage_users --scope host:*
# Secret prints exactly once — copy it now.
```

Rotate a leaked secret in place (access key ID stays the same):

```sh
basement keys rotate sa-abcdef...
```

## Config file

`~/.config/basement/config.yaml`:

```yaml
profiles:
  default:
    endpoint: https://basement.pq.io
    access_key_id: BMNT0000111122223333
    secret_key: <plaintext>
    current_region_id: 9e0eb...
  staging:
    endpoint: https://basement.staging.example.com
    access_key_id: BMNTaaaabbbbccccdddd
    secret_key: <plaintext>
```

The `secret_key` is stored in plaintext but the file is mode 0600.
For tighter posture, leave `secret_key` empty and pass
`$BASEMENT_SECRET_KEY` at invocation time.

## Architecture notes

- Bearer auth: every request carries
  `Authorization: Bearer {AccessKeyID}:{Secret}` — see
  [`internal/auth/bearer.go`](../../internal/auth/bearer.go) for the
  server-side middleware.
- Region operations resolve through
  [`internal/api/user_regions.go`](../../internal/api/user_regions.go);
  the CLI never speaks S3 directly. Object get/put presign first, then
  stream bytes to/from the backend.
- Service-account management is routed through
  [`internal/api/admin_service_accounts.go`](../../internal/api/admin_service_accounts.go)
  and requires the `host:manage_users` capability on the calling SA.

## Testing

```sh
go test -race ./cmd/basement/...
```

The CLI test suite uses `httptest` mock servers exclusively — running
the tests does not require a live basement deployment.
