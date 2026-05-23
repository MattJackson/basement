# Backing up basement itself

basement is a control plane: bucket contents live in the backend
(Garage / MinIO / AWS S3), and basement holds the metadata about
which user can reach which bucket, with which credentials, on which
backend. This page is about backing up that metadata layer — the
basement instance's own state.

> **Two different "backup" concepts in basement.** Don't confuse them.
>
> - **Scheduled bucket-to-bucket backups** (v1.5+, `/files/backups`)
>   — basement reads one bucket and writes to another on a cron.
>   This protects your **object data**.
> - **Backing up basement itself** (this doc) — copying
>   `BASEMENT_DATA_DIR` so you can restore the basement instance.
>   This protects your **basement metadata**.
>
> You need both. They're independent.

## What's in `BASEMENT_DATA_DIR`

`BASEMENT_DATA_DIR` (default `/var/lib/basement`) holds basement's
entire state:

| File | Contains | Sensitivity |
| --- | --- | --- |
| `users.json` | Local user accounts: usernames, bcrypt password hashes, roles, OIDC subject IDs | Medium (hashes, not plaintext) |
| `user_regions.json` | Per-user S3 credentials, AES-GCM-encrypted with a key derived from `BASEMENT_JWT_SECRET` | High (encrypted) |
| `connections.json` | Per-cluster admin endpoints + admin tokens, AES-GCM-encrypted | High (encrypted) |
| `bucket_grants.json` | Per-bucket grant records (legacy; retained for migration) | Low |
| `invites.json` | Outstanding invite tokens with their target roles | Medium |
| `shares.json` | Public share tokens with their target object/bucket | Medium |
| `oidc_group_mappings.json` | OIDC group → role mappings | Low |
| `org_capabilities.json` | Org-wide settings (elevation TTL, gateway toggles, etc.) | Low |
| `service_accounts.json` | M2M bearer credentials — AKIDs visible, secrets hashed | Medium (hashes) |
| `webhooks.json` | Bucket-event webhook subscriptions + signing secrets (plaintext) | High |
| `federated_buckets.json` | Multi-backend mirrored bucket records | Low |
| `backups.json` | Scheduled bucket-to-bucket backup job definitions | Low |
| `audit/YYYY-MM-DD.log` | Daily JSONL audit log files, append-only | Medium |

The AES-GCM-encrypted files are **useless without the JWT secret**.
Back up the secret separately (and protect it the same way).

## Backup procedure

The data files are written atomically (`write to tmp file → fsync →
rename`), so an instantaneous copy will always catch each file in a
self-consistent state. The risk is *across* files: a backup that
copies `connections.json` and then `users.json` 200ms later could
capture a partial multi-file transaction (rare but possible).

Two correct approaches:

### Option A — filesystem snapshot (recommended)

If the underlying filesystem supports atomic snapshots (ZFS, btrfs,
LVM, AWS EBS), take a snapshot of the data dir's volume and back up
from the snapshot. The snapshot is a single point-in-time view of
every file; cross-file consistency is preserved.

ZFS example:

```bash
# Take a snapshot.
sudo zfs snapshot tank/docker/basement-data@$(date -u +%Y%m%dT%H%M%SZ)

# Copy the snapshot to remote backup.
sudo rsync -aHAX --delete \
    /tank/docker/basement-data/.zfs/snapshot/<snap-name>/ \
    backup@backup.example.com:/srv/backups/basement/<date>/

# Drop the snapshot when done.
sudo zfs destroy tank/docker/basement-data@<snap-name>
```

btrfs and LVM follow the same shape (snapshot → rsync → destroy).
AWS EBS: snapshot the volume, mount the snapshot on another EC2,
copy out.

### Option B — stop basement first

If you can tolerate downtime, stop the container, copy, restart.
This guarantees no writes are in flight.

```bash
# Stop basement (cleanly flushes any in-progress write).
docker compose stop basement

# Copy the data volume to backup target.
docker run --rm \
    -v basement-data:/source:ro \
    -v /srv/backups/basement:/backup \
    alpine:3 \
    tar czf /backup/basement-data-$(date -u +%Y%m%dT%H%M%SZ).tar.gz -C /source .

# Restart.
docker compose start basement
```

For a 24/7 service this is too disruptive; use snapshots.

### Option C — live rsync (NOT recommended without a snapshot)

```bash
# Don't do this in production.
docker run --rm \
    -v basement-data:/source:ro \
    -v /srv/backups/basement:/backup \
    alpine:3 \
    rsync -a /source/ /backup/$(date -u +%Y%m%dT%H%M%SZ)/
```

This will *probably* be fine because basement's atomic-rename
writes mean each individual file is consistent. But if basement
performs a multi-file transaction (rare — most writes touch one
file) during the rsync, you could capture an inconsistent set.
Use only when you cannot snapshot.

## Don't forget the JWT secret

The encrypted columns (`user_regions.json`, `connections.json`) are
gibberish without `BASEMENT_JWT_SECRET`. Restoring the data dir to
a fresh basement instance with a different secret yields:

- No user can sign in (sessions invalidated — expected)
- No driver connection works (admin token decryption fails)
- Per-user S3 credentials are unreadable

**Back up the JWT secret with the same retention and access policy
as the data dir.** Two reasonable patterns:

1. **Keep the `.env` file in your existing secret store.** Vault, AWS
   Secrets Manager, Doppler, 1Password — wherever the rest of your
   ops secrets live. Restore the `.env` and the data dir together.
2. **Print the secret + admin password hash on paper and put it in a
   safe.** Cold backup for the "everything is on fire" case. Sounds
   silly, works.

## Restore procedure

> **Warning: do not restore over a running basement instance.**
> basement holds in-process state (active sessions, replication
> queues, pending webhook deliveries) that does not survive a
> blind file overwrite. Replacing `BASEMENT_DATA_DIR` mid-write
> WILL corrupt files and may corrupt the running instance's view
> of its own state.
>
> **Always stop basement first, restore, then start.**

The shape:

```bash
# 1. Stop the running basement instance.
docker compose stop basement

# 2. Wipe the existing data volume (or rename it, if you want a
#    rollback path before committing). Use whatever tool matches
#    your volume layout. For a named Docker volume:
docker volume rm basement-data
docker volume create basement-data

# 3. Restore the backup INTO the volume.
docker run --rm \
    -v basement-data:/target \
    -v /srv/backups/basement:/backup:ro \
    alpine:3 \
    sh -c 'cd /target && tar xzf /backup/basement-data-<TIMESTAMP>.tar.gz'

# 4. Confirm BASEMENT_JWT_SECRET in .env matches the secret that
#    was active when the backup was taken. If you rotated the
#    secret between backup time and now, you need the OLD secret
#    to decrypt the restored files. (Migrate to the new secret
#    via the rotation procedure in docker.md AFTER restore.)

# 5. Start basement.
docker compose start basement

# 6. Verify: sign in, check /admin/clusters lists your clusters,
#    check /admin/users lists your users, check /admin/audit shows
#    historical entries.
```

If basement comes up with an empty `/admin/clusters` or signs-in
fail, the most likely cause is **wrong JWT secret**. Stop, fix the
secret, start again.

## What's not backed up by this procedure

- **Object data in your S3 backends** — use the bucket-to-bucket
  backup feature at `/files/backups` (v1.5+) or your backend's
  native snapshot/replication
- **The basement container image** — pull the same tag from
  `ghcr.io/mattjackson/basement` and it's bit-identical
- **The Compose file + Caddyfile** — version-control these
  alongside your `.env`
- **TLS certs** (if you BYO) — Caddy's `caddy-data` volume holds
  Let's Encrypt account keys + certs; back that up if you can't
  tolerate Caddy fetching fresh certs on restore

## Restore drill

The restore procedure works only if you've tested it. At least
once per quarter:

1. Spin up a second basement instance on a non-prod hostname with
   the backup's data dir + matching JWT secret
2. Confirm you can sign in, the clusters list is intact, the users
   list is intact, and the audit log is intact
3. Tear the test instance down

If the restore drill ever fails, your real backup is broken — fix
the procedure before you need it.

## See also

- [`docker.md`](docker.md#volume-layout) — the data-dir layout
  with file purposes
- [`docker.md`](docker.md#rotating-basement_jwt_secret) — JWT secret
  rotation procedure (the related "what to do BEFORE you rekey"
  story)
- [`hardening.md`](hardening.md#data-directory) — file-permission
  hygiene on the data dir
- [`upgrade.md`](upgrade.md) — back up before upgrading
