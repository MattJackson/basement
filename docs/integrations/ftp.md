# FTP / SFTP gateway

> **Status:** stub. Registered in v1.9.0c so the `/admin/gateways`
> roster surfaces it from day one; the implementation is **not
> built and not on the v1.x roadmap**. FTP / SFTP sits in the
> long-tail v2.x line (see [ADR-0006](../adr/0006-v2-s3-gateway.md)
> for the v2.x sketch — SMB/NFS land in v2.3, FTP follows when
> demand surfaces).

## What it is

The FTP family (FTP, FTPS, SFTP) is the long tail of file-transfer
protocols. A working basement FTP gateway would let:

- Legacy clients (media-server boxes, embedded devices, decade-old
  upload tools) speak directly to basement without an external
  bridge.
- SFTP-only automation pipelines (a common shape in finance + media
  workflows) target basement as a drop directory.
- Operators run a deliberate FTPS endpoint for partner data exchange
  without standing up a separate FTP server.

## Why it's a stub

FTP has decent pure-Go libraries (`jlaffaye/ftp` for the client,
`goftp/server` for the server). The v1.9.0c scope was the gateway
abstraction itself, not a new protocol; FTP / SFTP got registered
as a stub so the operator UI surfaces every planned gateway from
day one. The full implementation ships in the v2.x line, after
the higher-demand SMB + NFS pair (v2.3).

SFTP via `golang.org/x/crypto/ssh` is the more interesting target.
We'd lump it under this gateway with a "preferred" badge for the
SSH-tunneled variant — the encryption story makes it the right
default for the modern case.

## What to use instead — today

For SFTP-only automation, run `sftpgo` as a sidecar against
basement's user-region S3 endpoint; the bridge is small + battle-
tested.

For plain FTP, no recommendation — the protocol is rarely the right
answer in 2026.

## Implementation tracking

The native FTP / SFTP gateway is in the v2.x long-tail. When it
ships, this doc will be replaced by the full integration guide
(mount flags, auth modes, SFTP vs FTPS recommendation,
troubleshooting). The gateway interface it'll implement is
documented in [adding-a-gateway.md](adding-a-gateway.md).
