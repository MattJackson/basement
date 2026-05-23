# SMB / CIFS gateway

> **Status:** stub. The SMB protocol is registered in basement's
> gateway registry so the `/admin/gateways` UI surfaces it as a
> placeholder, but no implementation ships today. SMB is the native
> Windows + macOS file-share protocol and would let Explorer / Finder
> mount basement directly and would unblock Time Machine targets;
> a production-grade pure-Go SMB server is the gating piece, and
> contributions are welcome &mdash; see
> [adding-a-gateway.md](adding-a-gateway.md) for the Gateway
> interface a contributed implementation would plug into. Until then,
> the supported pattern for SMB-bound clients is a Samba sidecar
> pointed at basement's S3 backend; for Time Machine specifically,
> see [time-machine.md](time-machine.md).
