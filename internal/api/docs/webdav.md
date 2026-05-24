# WebDAV integration

basement ships a native WebDAV gateway at `/webdav` (v1.9.0a). Any
WebDAV-capable client can mount your basement instance as a network
drive, list buckets per region, and read or write objects with the same
identity the web UI uses. No extra daemon, no sidecar.

This document walks through the per-platform mount flow, the
authentication surface, the known limitations of the v1.9 implementation,
and the common-case troubleshooting steps.

## Quick start

1. Sign into the basement web UI as a host admin.
2. Open `/admin/system` and confirm **Gateways → WebDAV → Enabled** is
   on. (It defaults to on; toggle off to kill-switch the gateway.)
3. Copy the **Mount URL** shown in the Gateways card. It defaults to
   `<your-origin>/webdav` — for example
   `https://basement.example.com/webdav`.
4. Follow the per-platform steps below.

If your basement instance sits behind a reverse proxy that exposes
WebDAV on a different external host, set the **Base URL override** in
the Gateways card. The card and these docs read the override; the
backend mount path itself is always `/webdav`.

## Connect from each platform

### macOS Finder

1. From the Finder, press **&#8984;K** (or choose **Go → Connect to
   Server…**).
2. Paste the mount URL: `https://basement.example.com/webdav`
3. Click **Connect**.
4. When prompted, choose **Registered User** and enter your basement
   username + password (or a service-account key pair — see below).

The mount appears under **Locations** in the Finder sidebar. Each
region you've registered shows up as a top-level folder; each bucket
under that region is a subfolder; objects are files inside the bucket.

> Tip: macOS aggressively caches Finder mounts. If a freshly created
> bucket isn't visible, eject the mount (right-click → Eject) and
> reconnect — Finder will issue a fresh PROPFIND.

### Windows Explorer

1. Open **File Explorer**, right-click **This PC** in the sidebar, and
   choose **Map network drive…**.
2. Pick a drive letter (any unused letter).
3. In **Folder**, paste the mount URL:
   `https://basement.example.com/webdav`
4. Tick **Connect using different credentials**, click **Finish**.
5. Enter your basement username + password when prompted.

Windows will mount basement as the drive letter you chose, available
from every Explorer window and from the `dir` / `copy` CLI as
`X:\<region>\<bucket>\…`.

### Linux (Nautilus / GNOME Files)

1. Open **Files**, click **Other Locations** in the sidebar.
2. At the bottom of the window, in **Connect to Server**, enter the
   URL with the `davs://` scheme (not `https://`):
   `davs://basement.example.com/webdav`
3. Click **Connect**, enter your basement username + password.

KDE Dolphin works the same way: **Network → Add Network Folder →
WebFolder (webdav)**.

For terminal-only setups, [`rclone`](https://rclone.org/) with the
[WebDAV remote](https://rclone.org/webdav/) is the most reliable
command-line client.

### iOS Files

1. Open the **Files** app.
2. Tap the **…** menu in the top-right of the Browse tab.
3. Choose **Connect to Server**.
4. Paste the mount URL: `https://basement.example.com/webdav`
5. Tap **Connect**, choose **Registered User**, enter basement
   credentials.

The mount appears under **Shared** in Files and is usable from any iOS
app that uses the system document picker.

### Android

The stock **Files by Google** app does not speak WebDAV. Use a
third-party client such as **Solid Explorer** or **CX File Explorer**
and choose WebDAV / WebDAVs as the connection type, pointing at the
mount URL.

## Authentication

basement accepts HTTP Basic auth on every WebDAV request. Two credential
shapes work in the same header:

1. **Username + password** — your basement account. Same credentials
   you'd use to sign into the web UI.
2. **Service account** — the `BMNT…` access-key goes in the **username**
   field; the shown-once secret goes in the **password** field. Mint a
   service account at `/admin/service-accounts`. This is the
   recommended flow for automation and for shared workstations where
   you don't want to type your personal password into a Finder prompt.

The gateway does NOT issue a session cookie — every WebDAV request
re-authenticates from scratch. This matches the S3 wire model (every
signed request is standalone) and keeps the gateway stateless.

## Limitations

The v1.9 gateway is intentionally minimal. Known gaps:

- **No LOCK / UNLOCK.** WebDAV class-2 locking is not implemented.
  Most read+write clients (Finder, Explorer, Nautilus, rclone) tolerate
  the absence of locking; clients that require LOCK (older Office
  versions, some CAD packages) will fail with 501 Not Implemented.
- **No per-bucket permissions enforcement at the WebDAV layer.** Access
  control is delegated to the backend access-key the region was
  registered with. If the registered key has read-only scope at the
  Garage / MinIO / S3 side, writes through WebDAV will fail with the
  backend's native error. basement does not re-implement an ACL on top.
- **Slow listings on large buckets.** PROPFIND on a bucket lists every
  object visible at the requested prefix. There is no server-side cache
  in v1.9 — clients that issue Depth: infinity against a large bucket
  will be slow. Most clients default to Depth: 1, which is fine.
- **No object versioning surface.** Old versions of an object are not
  visible through WebDAV even if the backend retains them.

## Troubleshooting

| Symptom | Likely cause | Fix |
| --- | --- | --- |
| `401 Unauthorized` on every request | Wrong username / password | Re-enter credentials, or mint a fresh service account |
| `403 Forbidden` with `GATEWAY_DISABLED` body | Operator flipped WebDAV off at `/admin/system` | Toggle it back on, or pick a different access path |
| `404 Not Found` on a known bucket | Wrong region alias in the URL | Confirm `/webdav` lists the region; the path is `/webdav/<region-alias>/<bucket>/…` |
| `501 Not Implemented` from the client | Client requires LOCK | Switch to a client that doesn't require WebDAV class-2 locking |
| Listings appear empty for a Garage backend | Admin connection not registered | basement uses the admin-connection bridge to list Garage buckets; register the Garage cluster at `/admin/clusters` |
| Slow first listing | Cold cache on a large bucket | Expected behaviour; subsequent listings reuse the client-side cache |

## See also

- [Service accounts](/admin/service-accounts) — mint a long-lived
  WebDAV credential without exposing a user password.
- [`docs/integrations/time-machine.md`](./time-machine.md) — why
  basement does not ship native SMB and what to use instead.
- [`/admin/system`](/admin/system) — the Gateways card lets the
  operator kill-switch WebDAV without re-deploying.
