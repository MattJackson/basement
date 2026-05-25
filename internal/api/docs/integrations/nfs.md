# NFS gateway

> **Status:** stub. The NFS protocol is registered in basement's
> gateway registry so the `/admin/gateways` UI surfaces it as a
> placeholder, but no implementation ships today. A working NFS
> gateway would let Linux hosts and NAS appliances mount basement
> buckets via `mount -t nfs ...` and consume them as Kubernetes
> PersistentVolumes; the open design question is how NFSv4's own
> identity model (`AUTH_SYS` uid/gid, `RPCSEC_GSS` Kerberos) maps
> onto basement's HTTP-tier identity without giving up per-bucket
> ACLs &mdash; contributions are welcome and the Gateway interface
> they'd plug into is documented in
> [adding-a-gateway.md](adding-a-gateway.md). Until then, Linux
> clients can mount via the WebDAV gateway (`davs2` / `gvfs` /
> `rclone mount`) and Kubernetes workloads can use `s3fs-fuse` or
> `geesefs` against a basement user-region S3 endpoint.
