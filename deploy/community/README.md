# Community release bootstrap

Explicitly non-expiring packages use signed `unirouter.upgrade-manifest.v2` with `expires_at: "never"` (UniRouter 1.14.0+). The standalone verifier and backend must both support v2, including `upgrade record-applied`. Existing finite v1 packages retain their original deadline; replacing or re-signing them cannot resume a plan bound to a different package hash. Every check/install/resume still verifies the original signature, hashes, creation time (with the existing five-minute future tolerance), platform, schema and release sequence. Non-expiring does not authorize rollback, skip trust checks or extend the owner's finite signing-authority window. This source change is not a published package or a verified deployment.

Current 1.13.0 delivery requires the M127 maintenance/fresh-install contract. See
[connection communication controls](../../docs/ops/XINSIGHT-COMMUNICATION.md) for manual defaults, migration and recovery. Earlier M126 candidate notes below remain historical.

**Unreleased 1.11.0 candidate.** No official bootstrap JSON, public package,
verifier hash or image set is published yet. Do not substitute the test fixtures
for an official release. See [installation guidance](../../docs/community/install-linux.md)
and the [current acceptance boundary](../../docs/ops/COMMUNITY-INSTALLER.md).

`bootstrap.py` downloads a pinned verifier, public ring and signed package,
checks their identities, prepares regular payload files, and calls the existing
customer installer. It does not introduce another signing format or database
installer. The first online bootstrap supports Linux amd64. The existing direct
verified-package path retains its source-level amd64/arm64 support.

## Initial trust

Obtain **this script, the bootstrap JSON and the expected SHA-256 values** from
the reviewed, commit-pinned official release instructions before running code.
Verify the script before execution. The JSON and its checksum beside an
untrusted download do not establish trust. The initial root is the authenticated
official repository/HTTPS channel; two URLs on the same GitHub account do not
protect against account compromise. No floating `latest` discovery is used.

Existing users with a separately trusted public ring can retain the direct
verifier/installer flow. This entry never updates an existing trust ring.
No private signing key belongs on the installation server.

## Maintainer-supplied bootstrap JSON

The strict `unirouter.community-bootstrap.v1` object contains:

| Field | Required content |
| --- | --- |
| `schema_version` | `unirouter.community-bootstrap.v1` |
| `platform` | Exactly `{"os":"linux","arch":"amd64"}` |
| `release` | Exact `version`, full source `commit`, positive `release_sequence`, `key_id`, and `target_schema` |
| `assets` | Exactly `verifier`, `public_keys`, and `package`; each has `url`, `size`, and `sha256` |

The target schema is `000126_commercial_license_retirement`. Asset URLs must
use credential-free HTTPS on port 443; redirects must also remain HTTPS. Hashes
are lowercase SHA-256 and sizes are exact byte counts. Limits are 32 KiB for the
bootstrap, 64 KiB for the ring, 32 MiB for the verifier and 512 MiB each for the
compressed online package and its total payload. Duplicate/unknown JSON fields
are rejected. These online limits do not change the existing offline format.

There is deliberately no populated sample identity that looks installable.
Allocate real release coordinates only after reviewing the actual history,
approved signing key, source and artifacts.

A release offering an upgrade delta uses `unirouter.community-bootstrap.v2`.
It preserves all v1 fields and adds one `delta` object. Its `basis` is the
exact retained package's `size` and `sha256`. Its `asset` uses the normal
HTTPS `url`, `size` and `sha256` fields. Publish these identities in the same
independently pinned bootstrap. The sidecar has a 32 MiB compressed limit.
It carries the original signed target metadata and changed TAR members.

## Prepare an upgrade using a retained package

Use the source package named by the installed release history. Select delta
preparation explicitly by adding `--source-package /absolute/current.tar.gz`
to the ordinary `prepare` command. To reuse a retained verifier that matches
the new bootstrap's exact identity, also pass
`--source-verifier /absolute/previous-preparation/uniroute-upgrade`. When the
verifier changes, preparation downloads the pinned target binary. The same
prepared full package then works with the existing `check-upgrade` and
`upgrade` commands, with their original source package and admission checks.

After a transfer interruption, repeat the same `prepare` command in the
same private directory. It resumes only the same pinned asset with an exact
HTTP 206 response. For an absent or incorrect basis, unusable delta, or a
server that cannot continue it, keep the original directory for inspection.
To prepare the full release, use a new private directory and omit both
source options. Fresh installation uses the ordinary full package.

Maintainers run `uniroute-upgrade create-delta --basis OLD --target NEW
--public-keys RING --current-schema SCHEMA --out DELTA` against the two exact
signed archives. The native tool verifies both archives, derives the
member inventory, and proves the round trip reproduces the target SHA-256
and size before producing the sidecar. It prints the identities used in the
bootstrap. Use the release's locked Go toolchain, retain all full-package
license materials, and qualify reconstruction with the shipped verifier.
Docker pulls remain part of the existing installer or upgrader.

## Actions

All preparation and installation commands should run as the **same root user**
under a root-controlled path such as `/opt/unirouter-release`. The tool refuses
to reuse another user's directory under sudo and never changes ownership.
The parent must already exist. Python 3.10+, Bash and the existing local rootful
Docker/Compose prerequisites still apply; Docker is not installed automatically.

After the real release instructions supply the files and trusted hash:

```sh
# Set BOOTSTRAP_SHA256 to the independently verified release value first.
sudo python3 -I -B bootstrap.py prepare \
  --bootstrap bootstrap.json --bootstrap-sha256 "$BOOTSTRAP_SHA256" \
  --directory /opt/unirouter-release

sudo python3 -I -B bootstrap.py check \
  --bootstrap bootstrap.json --bootstrap-sha256 "$BOOTSTRAP_SHA256" \
  --directory /opt/unirouter-release

sudo python3 -I -B bootstrap.py install \
  --bootstrap bootstrap.json --bootstrap-sha256 "$BOOTSTRAP_SHA256" \
  --directory /opt/unirouter-release
```

`install` can also prepare a new release directory itself, avoiding the separate
`prepare` invocation. `check` requires completed preparation and invokes the
existing installer's **new-install** prerequisite check without downloads or
writes. It is not an installed-instance health check. `prepare` never executes
the downloaded installer or creates installation credentials/services.

`--directory` retains the prepared release; `--install-directory` selects runtime
data (default `/opt/unirouter`). `--name`, `--port` and `--docker-socket` forward
to the existing installer unchanged. Keep the original arguments and use
`resume` in place of `install` after an interrupted installation. Resume never
downloads, replaces prepared files or starts a new installation plan. It still
checks the original package's current expiry; an expired package requires the
supported recovery procedure, not a clock override or a newly signed substitute.

## Failure and verification behavior

Download hashes are checked before executing the verifier. A bounded physical
USTAR scan rejects links, extension headers, unsafe paths, duplicate/undeclared
entries and excessive decompression before signature verification. The existing
Go verifier then authenticates the package. Only its bound manifest determines
release identity and extracted bytes. Preparation never invokes tar extraction
helpers or honors archive file permissions. Directories are private (0700),
payload files 0600, and only the hash-verified verifier is executable (0700).

On reuse, the bootstrap/asset hashes, signature, expiry and all payload files
are checked again. `prepared.json` indicates completed preparation; it is not
an independent trust authority. Repeat `prepare` with the same pinned bootstrap
and private directory to continue a partial asset-download stage. Complete
assets are checked before reuse; a partial asset requires a matching HTTP 206
range response and passes the full expected size/hash before execution. There
is no automatic retry. Changed bootstrap inputs, corrupt complete assets,
foreign-owned files and incomplete extraction refuse reuse and remain available
for diagnosis. Resume stays bound to the same pinned asset. For cross-version
upgrades, bootstrap v2 reuses matching members from a verified complete
predecessor archive. Partial downloads cannot be combined across versions.
No automatic deletion or server cleanup is added.

Offline tests cover transport/redirect refusal and archive attacks. A Go test
uses the real verifier and synthetic signed packages, including expired/wrong
key cases and compatibility with the existing installer. The native GitHub
harness additionally prepares its synthetic package through this entry before
the original installation/recovery tests. Fixture transport does not prove
public HTTPS availability; official download and image-pull acceptance remains
bound to the eventual published artifacts.
