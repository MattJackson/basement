# NFS gateway

> **Status:** stub. Registered in v1.9.0c so the `/admin/gateways`
> roster lists every protocol on the v1.10+ roadmap; the
> implementation is not built yet.

## What it is

NFS (Network File System) is the lingua franca of Linux + most NAS
appliances. A working basement NFS gateway would let:

- Linux hosts mount basement buckets via `mount -t nfs ...` without
  webdav-fuse or rclone-mount.
- NAS appliances export basement buckets to LAN clients with their
  native UI.
- Container orchestrators consume basement buckets as NFS
  PersistentVolumes.

## Why it's a stub

NFS v4 has decent pure-Go server libraries (e.g. `go-nfs`), but the
integration with basement's identity surface is the v1.10 cycle's
work. NFSv4 carries its own user-id model (`AUTH_SYS` numeric uid/gid;
`RPCSEC_GSS` Kerberos) that doesn't map onto basement's HTTP-tier
identity. Bridging that without giving up basement's per-bucket ACL
story is the open design question.

## What to use instead — today

For Linux clients, the WebDAV gateway works via `davs2` / `gvfs` /
`rclone mount`. The performance is worse than NFS for many-small-file
workloads, but the auth + ACL story is solid.

For Kubernetes workloads, `s3fs-fuse` or `geesefs` against basement's
user-region S3 endpoint is the supported pattern.

## Implementation tracking

The native NFS gateway is planned for v1.10. When it ships, this doc
will be replaced by the full integration guide (mount flags, auth
mapping, performance notes, troubleshooting). The gateway interface
it'll implement is documented in [adding-a-gateway.md](adding-a-gateway.md).
