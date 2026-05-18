# basement

**OSS-quality admin UI for [Garage](https://garagehq.deuxfleurs.fr), the
self-hosted S3-compatible object storage system.**

> Status: 🚧 Pre-code, design phase. v1.0 target: 9 screens covering every
> Garage admin API operation, single-binary Docker image, dark-mode-default,
> Linear/Vercel-tier visual polish.

## Why

The existing community admin UI for Garage stops working on Garage v1.0+
(API path changes — calls `/cluster/status` where Garage v1 only serves
`/v1/cluster/status`). Garage's own admin UI is on the roadmap but with
no timeline. This project fills the gap with a small, well-scoped, well-
maintained alternative.

## Planned stack

- **Backend:** Go (single static binary, ~10MB image, holds the Garage
  admin token, proxies to the SPA)
- **Frontend:** React + Vite + TypeScript + Tailwind + [shadcn/ui](https://ui.shadcn.com)
- **Auth:** username + bcrypt password → httpOnly JWT cookie (single-
  admin in v1.0; OIDC SSO planned for v1.x)
- **Distribution:** single Docker image, multi-arch (amd64 + arm64),
  published to `ghcr.io/mattjackson/basement`

## Scope (v1.0)

✅ Dashboard with cluster health<br>
✅ Cluster: node list + layout editor (drag-and-drop role assignment,
diff preview, stage/apply/revert)<br>
✅ Buckets: CRUD, aliases, quotas, key-permission grid<br>
✅ Keys: CRUD, bucket-permission grid (transposed view of the same data)<br>
✅ Status / Diagnostics: every Garage admin endpoint exposed<br>

❌ Object browsing — use Cyberduck, `mc`, or `aws s3` (Garage is S3-
compatible; any S3 client works)<br>
❌ Multi-cluster management (one Garage cluster per instance)<br>
❌ Metrics dashboards (use the Prometheus endpoint Garage already exposes)<br>

## License

MIT. See [LICENSE](LICENSE).

## Contributing

Once v0.1 lands. For now, design discussion welcome in GitHub Discussions
when they're opened.
