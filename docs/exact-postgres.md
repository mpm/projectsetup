# Pin a PostgreSQL sidecar image

The built-in `postgres` add-on accepts a dedicated `--postgres-image` reference while keeping `--postgres-version` as its explicit data-compatibility major. For example (illustrative version, verify the artifact before starting it):

```bash
projectsetup init --non-interactive --preset node --database postgres \
  --postgres-version 18 --postgres-image postgres:18.1-trixie
projectsetup check
```

For immutable artifact selection, append `@sha256:` followed by the actual 64 lowercase hexadecimal digest to the tagged reference. Obtain and verify that digest separately; projectsetup never resolves tags or fetches images during generation, ordinary checking, or upgrade. An exact patch tag alone can still be republished; a digest fixes the artifact.

Accepted syntax is `repository:MAJOR[.PATCH][-VARIANT][@sha256:DIGEST]`. Repository components use lowercase letters/digits and Docker dot, underscore, double-underscore, or hyphen separators; a registry hostname (or bracketed IPv6 address) and numeric port are optional. Tags have at most 128 characters and repository names at most 255. Moving tags such as `latest`, digest-only references, interpolation, credentials, malformed digests, and a tag whose major disagrees with `postgres.version` are rejected. The patch component names PostgreSQL's minor maintenance release, commonly called a patch release in image pinning. Select a Debian/glibc artifact compatible with the built-in image contract.

Validation establishes syntax and consistency of your declared major. It cannot establish that a mirror contains PostgreSQL, that a tag and digest correspond, or that an artifact supports your host architecture. The explicitly requested container tooling performs artifact retrieval when you build/start it. The flag applies only to the built-in `postgres` add-on; custom add-ons declare their own service images. It is a typed image field, not a generic `--set` option, and generic option character restrictions remain unchanged.

The major still selects the built-in named-volume target:

| Recorded major | `postgres-data` target |
| --- | --- |
| Before 18 | `/var/lib/postgresql/data` |
| 18 and later | `/var/lib/postgresql` |

An explicit reference changes the sidecar image only. It retains the selected major's mount layout, environment, healthcheck, and app dependency. Without an explicit reference, the existing defaults remain `postgres:MAJOR-bookworm` before 18 and `postgres:MAJOR-trixie` from 18 onward.

## Preserve or replace the selection

Manifest schema 2 gains optional `postgresImage`; `options.postgres.version` retains the major. The new field is omitted when unused. Existing schema 1/2 manifests, built-in definition bytes/hashes, and major-only generated output remain compatible. Schema 1 still defaults an unrecorded PostgreSQL major to 17. Older strict manifest readers reject the new field rather than discarding the pin.

Ordinary `projectsetup upgrade` preserves the reference, major, and exact definition snapshots offline. `upgrade --refresh-presets` adopts registry definitions explicitly while retaining the recorded reference and major. `init --force` retains both for a selected PostgreSQL add-on unless you explicitly replace them. To change the reference, pass `--postgres-image NEW_REFERENCE`; to return to the definition's major-only default, pass `--postgres-image ''`. A retained reference that disagrees with an explicitly changed major fails before replacing generated files; provide a matching reference or explicitly clear it. Removing the add-on removes the recorded reference.

Recreate containers deliberately after changing the selection. Regeneration writes configuration files; it does not migrate, rename, empty, or delete database volumes, nor does it start or stop the database. Keep the project name stable when retaining its existing Compose-scoped volume.

## Plan major upgrades separately

Changing the configured major does not convert existing data. Plan and test a separate database migration, retaining backups and the original volume until verified. PostgreSQL documents logical dump/restore and `pg_upgrade` as major-upgrade methods, with application and extension compatibility checks. See [PostgreSQL cluster upgrades](https://www.postgresql.org/docs/current/upgrading.html). Choose the migration procedure for your actual source/target releases and container layouts; projectsetup does not run it.
