# SMB / CIFS gateway

> **Status:** stub. Registered in v1.9.0c so the `/admin/gateways`
> roster surfaces it from day one; the implementation is **not
> built and not on the v1.x roadmap**. SMB ships as part of the v2.3
> "SMB + NFS gateways alongside WebDAV" line (see
> [ADR-0006](../adr/0006-v2-s3-gateway.md) for the v2.x sketch).

## What it is

SMB (Server Message Block) is the native Windows + macOS file-share
protocol. A working basement SMB gateway would let:

- Windows Explorer mount basement as a network drive without WebDAV.
- macOS Finder mount basement via the `smb://` scheme.
- macOS Time Machine target basement directly (Time Machine refuses
  WebDAV; SMB is its only supported network destination).

## Why it's a stub

A production-grade SMB server in pure Go does not exist today. The
Microsoft protocol surface is large (SMB2, SMB3, leases, oplocks,
AAPL extensions, multichannel, signing, encryption) and the
open-source Go libraries cover at best a subset. Time Machine in
particular is sensitive to SMB semantics that the partial Go
implementations get wrong, leading to silent backup corruption that
surfaces months later when a restore is attempted.

basement is single-binary on purpose. We don't want to bolt a partial
Samba reimplementation in just to check the SMB box.

## What to use instead — today

For Time Machine backups against basement, see
[time-machine.md](time-machine.md): the supported pattern is a Samba
sidecar pointed at basement's S3 backend, or basement's BACKUP wizard
pointed at a NAS volume.

For SMB-only legacy apps, the same Samba-sidecar pattern works — the
sidecar bridges SMB → S3 → basement.

## Implementation tracking

The native SMB gateway is in the v2.x line (planned for v2.3
alongside NFS). When it ships, this doc will be replaced by the
full integration guide (per-client mount instructions, auth mapping,
known limitations, troubleshooting). The gateway interface it'll
implement is documented in [adding-a-gateway.md](adding-a-gateway.md).
