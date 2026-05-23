# Upgrade procedure

basement releases are tagged in the `ghcr.io/mattjackson/basement`
registry. An upgrade is "pull the new tag, restart the container".
Within a major version (v1.x), basement guarantees forward and
backward compatibility of the on-disk data format — you can roll
between v1.10 and v1.11 freely.

## Before you upgrade

1. **Back up `BASEMENT_DATA_DIR`** — see
   [`backup-basement.md`](backup-basement.md). This is the single
   non-negotiable step. If the upgrade goes wrong, you restore from
   here.
2. **Read the release notes** for the version you're upgrading to.
   They live in [`../release-notes/`](../release-notes/) and call
   out any breaking changes or operator action required (config
   changes, env-var renames, capability migrations).
3. **Pin the version you're upgrading FROM** — note the current
   `:tag` so you can roll back to it if needed.

## Manual upgrade — Docker Compose

The standard recipe:

```bash
# 1. Back up first.
docker run --rm \
    -v basement-data:/source:ro \
    -v /srv/backups/basement:/backup \
    alpine:3 \
    tar czf /backup/basement-data-pre-v1.11.0-$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /source .

# 2. Edit docker-compose.yml — bump the image tag.
#    image: ghcr.io/mattjackson/basement:v1.10.0
#    becomes
#    image: ghcr.io/mattjackson/basement:v1.11.0

# 3. Pull the new image without restarting yet.
docker compose pull basement

# 4. Recreate the container with the new image. This stops the old
#    container and starts the new one in the same step; downtime is
#    typically 2-5 seconds for the basement service.
docker compose up -d basement

# 5. Confirm the new version is running.
docker compose logs --tail 20 basement
# Look for the startup line — basement logs its version + commit.

# 6. Smoke check: open the UI, sign in, confirm /admin/clusters
#    still lists your clusters, /admin/users still lists users.
```

## Watchtower (auto-upgrade on `:latest`)

If you'd rather not chase release tags by hand,
[Watchtower](https://containrrr.dev/watchtower/) polls the registry
and pulls + recreates containers when a new image is pushed.

Add Watchtower to your Compose file:

```yaml
services:
  basement:
    image: ghcr.io/mattjackson/basement:latest   # NOT a pinned tag
    # ... (rest of the service definition)
    labels:
      # Tell Watchtower this container is in scope.
      com.centurylinklabs.watchtower.enable: "true"

  watchtower:
    image: containrrr/watchtower:latest
    container_name: watchtower
    restart: unless-stopped
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    environment:
      # Only update containers with the enable label.
      WATCHTOWER_LABEL_ENABLE: "true"
      # Poll every 6 hours.
      WATCHTOWER_POLL_INTERVAL: "21600"
      # Clean up old images after a successful update.
      WATCHTOWER_CLEANUP: "true"
      # Don't restart unrelated containers.
      WATCHTOWER_INCLUDE_RESTARTING: "false"
```

Trade-offs:

- **Pros:** zero-touch security patches; you're always on the latest
  release within hours of publish
- **Cons:** an upgrade that requires operator action (rare in v1.x
  but possible at v1 → v2) will happen automatically, possibly
  during a maintenance window you didn't pick
- **Mitigation:** subscribe to release notifications (GitHub
  "Watch → Releases" on the repo), so you see a release coming
  before Watchtower applies it

For production environments where unattended upgrades are not OK,
**don't use Watchtower** — pin a tag and upgrade by hand. The
release-notes-first habit is more important than the convenience.

## Rollback

If the new version misbehaves, roll back:

```bash
# 1. Edit docker-compose.yml — restore the previous image tag.
# 2. Pull (if not still in local cache).
docker compose pull basement
# 3. Recreate.
docker compose up -d basement
```

Because basement maintains backward compatibility of the on-disk
format within v1.x, a forward-then-backward roll within the same
major version is safe. If you've already let the new version write
to the data dir, the old version reads it (it ignores unknown
fields).

If rollback after a long period of new-version writes ever feels
unsafe, restore the backup you took at step 1 above.

## Migration notes

### Within a major version (v1.x ↔ v1.x)

- **Forward and backward compatible.** No data migration step. No
  config changes required (unless explicitly called out in release
  notes).
- **New features may add fields to existing files.** Older versions
  ignore the new fields; newer versions populate them lazily.
- **New env vars** for new features are always optional with safe
  defaults. Don't have to set them to upgrade; set them later when
  you turn the feature on.

### Across major versions (v1.x → v2.x)

- **Read the v2.0 release notes carefully.** Major versions are
  where breaking changes land.
- **Stop on v1.x's latest release** (`v1.x.last`) before upgrading
  to `v2.0.0`. Don't try to skip from `v1.5` straight to `v2.0` —
  go through `v1.x.last` first so the data dir is in the shape
  v2.0 expects.
- **Take a backup specifically labelled "pre-v2.0"** and keep it
  longer than your normal retention. Major-version rollback may
  require a data-dir restore.
- **The v2.0 release will ship its own migration guide** in
  `docs/migrations/v1-to-v2.md` (planned). Until then, treat v2.0
  as a documented event, not a click-through upgrade.

### Specific historical migrations within v1.x

These have all been handled transparently in past releases; listed
here for awareness:

- **v1.1.0** — Region-tier user model. `bucket_grants.json` is
  retained for migration; live state moved to `user_regions.json`.
  No operator action.
- **v1.2.0** — Sudo-style admin elevation. Org-wide elevation TTL
  config added at `/admin/system`; default is 15 minutes.
- **v1.8.0** — Project rebrand `basement-ui` → `basement`. Image
  moved from any pre-v1.8 path to `ghcr.io/mattjackson/basement`.
  Module path changed to `github.com/mattjackson/basement`. License
  changed MIT → AGPLv3.
- **v1.9.0** — Gateways nest in `org_capabilities.json`. Legacy
  files migrate transparently in `OpenOrgCapabilities()` on load;
  no operator action.
- **v1.10.0** — Versioning + Object Lock + SSE. Bucket settings
  surface new cards but no data-dir changes; existing buckets keep
  their previous state.

## Health check after upgrade

The post-upgrade smoke check, in order of cost:

1. **`docker compose ps basement`** — the container is running, not
   restarting in a crash loop
2. **`docker compose logs --tail 50 basement`** — no `level=error`
   lines, the `serving on :8080` startup banner is the most recent
   line
3. **`curl -s https://basement.example.com/api/v1/version`** —
   returns JSON with the version + commit you expected
4. **Sign in to the UI** — auth still works
5. **Visit `/admin/clusters`** — your clusters list is intact
6. **Visit `/admin/users`** — your users list is intact
7. **Visit `/admin/audit`** — historical entries are intact, and
   you see new entries from your post-upgrade clicks

If any of these fail, roll back to the previous tag and check the
release notes for a step you missed.

## See also

- [`backup-basement.md`](backup-basement.md) — back up before upgrading
- [`docker.md`](docker.md) — the Compose file you're tag-bumping
- [`../release-notes/`](../release-notes/) — per-version release notes
- [`../../CHANGELOG.md`](../../CHANGELOG.md) — terse cycle-by-cycle log
