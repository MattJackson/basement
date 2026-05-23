# FTP / SFTP gateway

> **Status:** stub. The FTP / SFTP family is registered in basement's
> gateway registry so the `/admin/gateways` UI surfaces it as a
> placeholder, but no implementation ships today. A working
> gateway would let legacy clients, embedded devices, and SFTP-only
> automation pipelines target basement directly without an external
> bridge; pure-Go libraries exist for both FTP (`goftp/server`) and
> SFTP (`golang.org/x/crypto/ssh`), and contributions are welcome
> &mdash; the Gateway interface a contributed implementation would
> plug into is documented in
> [adding-a-gateway.md](adding-a-gateway.md). Until then, the
> supported pattern for SFTP-bound clients is to run `sftpgo` as a
> sidecar against basement's user-region S3 endpoint.
