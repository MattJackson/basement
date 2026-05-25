# Time Machine + SMB integration

> **basement does not natively support SMB.** This document explains
> why, and what we recommend instead for the two most common reasons
> operators ask: macOS Time Machine backups, and legacy SMB-only apps.

## Why no native SMB

SMB is the only protocol macOS Time Machine speaks for network targets.
Operators asking "can basement be a Time Machine destination?" are
asking, in practice, "can basement speak SMB?"

We deliberately do not ship an SMB server inside the basement binary:

- A production-grade SMB implementation in pure Go does not exist
  today. The Microsoft protocol surface is large (SMB2, SMB3, leases,
  oplocks, AAPL extensions, multichannel, signing, encryption) and the
  open-source Go libraries cover at best a subset.
- Time Machine in particular is sensitive to SMB semantics that the
  partial Go implementations get wrong, leading to silent backup
  corruption that surfaces months later when a restore is attempted.
- The right answer for serving SMB in 2026 is Samba — a hardened,
  decades-old daemon that already speaks every variant Apple, Microsoft,
  and the Linux kernel emit.

basement is single-binary on purpose. We don't want to bolt a partial
Samba reimplementation in just to check the SMB box.

## What to use instead

### Option 1 (recommended): basement BACKUP wizard pointed at a NAS

This is the path we recommend for Mac data:

1. Run Time Machine against a real SMB-speaking NAS (Synology,
   QNAP, TrueNAS, asustor, an old Mac mini running macOS Server, etc.)
   — the way Apple intended.
2. From `/user/backups` in basement, schedule a recurring backup of
   the NAS volume that holds the Time Machine sparsebundle. basement
   pulls the sparsebundle off the NAS over its native protocol and
   uploads it into your basement cluster off-site.

This gives you the strong local-restore path (Time Machine over SMB to
the NAS) plus an off-site copy on basement, without basement having to
speak SMB itself. The Time Machine snapshots themselves are not
basement objects — they're regular sparsebundle files inside a backup
job basement runs.

### Option 2: Samba sidecar reading basement over S3

If you specifically want one box that serves both basement web/WebDAV
and SMB, run Samba as a sidecar container that mounts a basement bucket
via `s3fs-fuse`. Caveats inline below.

A minimal compose snippet:

```yaml
services:
  basement:
    image: ghcr.io/mattjackson/basement:latest
    # ... your usual basement config

  samba:
    image: dperson/samba
    cap_add:
      - SYS_ADMIN
    devices:
      - /dev/fuse
    security_opt:
      - apparmor:unconfined
    volumes:
      - samba-mount:/mnt/basement:shared
    command: >
      -p
      -s "timemachine;/mnt/basement;yes;no;no;all;none"
      -u "tm;StrongPasswordHere"
    depends_on:
      - basement
      - s3fs

  s3fs:
    image: efrecon/s3fs
    cap_add:
      - SYS_ADMIN
    devices:
      - /dev/fuse
    security_opt:
      - apparmor:unconfined
    environment:
      AWS_S3_BUCKET: timemachine
      AWS_S3_URL: http://basement:3900
      AWS_S3_ACCESS_KEY_ID: ${BASEMENT_TM_ACCESS_KEY}
      AWS_S3_SECRET_ACCESS_KEY: ${BASEMENT_TM_SECRET_KEY}
    volumes:
      - samba-mount:/opt/s3fs/bucket:shared

volumes:
  samba-mount:
```

Point Time Machine at `smb://samba.local/timemachine` and authenticate
as `tm` / `StrongPasswordHere`.

## Caveats (read these before deploying option 2)

- **Time Machine on S3-backed SMB is fragile.** macOS expects fast,
  consistent random-access writes into the sparsebundle. S3-backed
  FUSE mounts add latency and weak consistency that Time Machine
  notices. You will see "Time Machine couldn't complete the backup"
  errors more often than with a native NAS target.
- **Sparsebundles are tens of thousands of band files.** Each band is
  a small (~8MB) object. Listing the sparsebundle directory makes
  many S3 LIST calls; on a cold cache this is slow.
- **Restore is the moment of truth.** Test a full restore from this
  setup before relying on it. We recommend option 1 for production.
- **basement does not test this configuration in CI.** The sidecar
  pattern is documented but unsupported by the basement project.
  Open issues against `dperson/samba` or `s3fs-fuse` upstream.
- **Not all SMB clients tolerate FUSE-S3 latency.** Legacy Windows
  apps that mmap files (some accounting software, some CAD packages)
  may corrupt files via the SMB-on-FUSE-on-S3 stack. Native NAS is
  the only safe answer for those workloads.

## Not supported disclaimer

The basement project does not officially support running Time Machine
against an SMB-via-Samba-sidecar bucket. The snippet above is a
community starting point. If you need a supported, tested Time Machine
target use a NAS and back the NAS up to basement (option 1).

## See also

- [`docs/integrations/webdav.md`](./webdav.md) — the protocol basement
  *does* ship natively, including from the macOS Finder.
- [`/user/backups`](/user/backups) — the basement BACKUP wizard
  (v1.5.0a) used in option 1.
- [`/admin/system`](/admin/system) — the Gateways card; the SMB
  section there links here.
