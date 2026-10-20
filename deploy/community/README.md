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
an independent trust authority. Failed downloads/preparation retain a partial
directory. The tool will not overwrite it, reset it or call it resumable.
Preserve files for diagnosis. No automatic deletion, server cleanup or upgrade
of an existing installation is included.

Offline tests cover transport/redirect refusal and archive attacks. A Go test
uses the real verifier and synthetic signed packages, including expired/wrong
key cases and compatibility with the existing installer. The native GitHub
harness additionally prepares its synthetic package through this entry before
the original installation/recovery tests. Fixture transport does not prove
public HTTPS availability; official download and image-pull acceptance remains
bound to the eventual published artifacts.
