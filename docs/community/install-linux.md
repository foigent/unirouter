# Install on a Linux server

Explicitly non-expiring packages use signed `unirouter.upgrade-manifest.v2` with `expires_at: "never"` (UniRouter 1.14.0+). The standalone verifier and backend must both support v2, including `upgrade record-applied`. Existing finite v1 packages retain their original deadline; replacing or re-signing them cannot resume a plan bound to a different package hash. Every check/install/resume still verifies the original signature, hashes, creation time (with the existing five-minute future tolerance), platform, schema and release sequence. Non-expiring does not authorize rollback, skip trust checks or extend the owner's finite signing-authority window. This policy alone does not establish public availability or a verified deployment.

[简体中文](install-linux.zh-CN.md) · [First model request](getting-started.md)

**Linux amd64 preview: v1.14.0-preview.12, source
`c89e7c60ac63204e990f52ec128cd493f185112b`, M127, release sequence 12.**
Use these fixed downloads only after the [release readiness](release-readiness.md)
publication and anonymous-installation checks pass. The actual package's signature
and all 24 payloads have been verified locally; public availability and installation
are separate gates. The [formal acceptance workflow](https://github.com/foigent/unirouter/actions/workflows/community-release-acceptance.yml)
records the real public-package result. Earlier
[synthetic native results](../ops/COMMUNITY-INSTALLER.md#accepted-native-bootstrap-result--2026-10-11)
retain their recorded scope. This entry installs a new instance; it does not upgrade
an existing server. Cloning the repository does not supply signed release assets.

The installer prepares PostgreSQL, Redis, UniRouter and its monitoring services,
generates the required local secrets, initializes the database and opens a
private management console. You do not need Go, Node.js or npm to compile UR.
After installation, use the console to configure your organization and model.
X-Insight is optional and is not required to install UniRouter.

## Before you start

Use a new installation directory and a new Docker Compose project. The first
online bootstrap candidate supports Linux amd64. The existing direct prepared-
release path retains source-level amd64/arm64 support; the public arm64 artifact
still needs native acceptance. You need:

- Python 3.10 or later, Bash, `curl` and `sha256sum`;
- a local, rootful Docker Engine with Compose v2, supporting `up --wait` and
  `config --environment`; see the [official Linux Compose instructions](https://docs.docker.com/compose/install/linux/);
- sudo access, a free loopback port (3000 by default), and enough disk for the
  release, six container images and retained database data;
- network access to the exact image registries declared by the release;
- the release directory, release-matched trusted `uniroute-upgrade` verifier,
  and public-key ring obtained through the release's independent trust process.

The first version does not install Docker or change server firewall rules.
It checks for an existing Compose project and Docker subnet conflicts. Its
file-space check does not estimate image size or future database growth.
The directory is private to root. The management port binds to `127.0.0.1`;
PostgreSQL and Redis have no published host ports.

## Download and install this preview

Obtain this guide through the authenticated official repository and review the
[preview release](https://github.com/foigent/unirouter/releases/tag/v1.14.0-preview.12)
before executing downloaded code. The script is pinned to the product source;
the JSON pins the verifier, public ring and signed package. A checksum beside
an untrusted download does not independently establish trust. This entry does
not replace an existing public ring or require a private signing key.

| Download | Bytes | SHA-256 |
| --- | ---: | --- |
| `bootstrap.py` at source c89e7c60 | 23,287 | `054c51a671a9255fd3a587fc7897d51fff0525e2e4ae54bb3926ac2e44785e3f` |
| `community-bootstrap.json` | 1,121 | `23985fa7f2728f7ee3e0a55d41030edc6c607c467b921f85de5f69a8f046ddb8` |
| `uniroute-upgrade-linux-amd64` | 4,099,528 | `60acbb428d746bccaea3e3bc0113e2d0f900b15057bda39455805f0e9a1bbd23` |
| `public-keys.json` | 83 | `96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d` |
| `unirouter-1.14.0-linux-amd64.tar.gz` | 130,031,729 | `c2e98cdc5b13d8bff745eaa7f576fb4bc2b7dda92a831a0ef1e01b148ee3f6e0` |

After the public release gates pass, open a root shell with `sudo -i`, then run
the block below. It requires unused download, preparation and runtime paths;
keep them for recovery. A download error or a checksum failure must stop before
the installer executes.

```sh
set -eu
umask 077
mkdir -m 700 /opt/unirouter-downloads-1.14.0
cd /opt/unirouter-downloads-1.14.0
curl --fail --location --proto '=https' --proto-redir '=https' \
  --output bootstrap.py \
  https://raw.githubusercontent.com/foigent/unirouter/c89e7c60ac63204e990f52ec128cd493f185112b/deploy/community/bootstrap.py
curl --fail --location --proto '=https' --proto-redir '=https' \
  --output community-bootstrap.json \
  https://github.com/foigent/unirouter/releases/download/v1.14.0-preview.12/community-bootstrap.json
printf '%s  %s\n' \
  054c51a671a9255fd3a587fc7897d51fff0525e2e4ae54bb3926ac2e44785e3f bootstrap.py \
  23985fa7f2728f7ee3e0a55d41030edc6c607c467b921f85de5f69a8f046ddb8 community-bootstrap.json \
  | sha256sum --check --status
python3 -I -B bootstrap.py install \
  --bootstrap community-bootstrap.json \
  --bootstrap-sha256 23985fa7f2728f7ee3e0a55d41030edc6c607c467b921f85de5f69a8f046ddb8 \
  --directory /opt/unirouter-release
```

The bootstrap verifies and prepares `/opt/unirouter-release`, then installs into
`/opt/unirouter` with Compose project `unirouter` and loopback port 3000 by default.
To review prerequisites first, use `prepare`, then `check`, then `install` with
the same arguments. Preparation creates no installation secrets or services;
`check` after preparation makes no changes or downloads. Keep the same root
identity and root-controlled paths throughout.

If installation is interrupted, do not run the download block again. Keep its
original JSON/hash, prepared release, database volumes and runtime directory.
From the same root shell and download directory, replace `install` with `resume`
in the final invocation; retain any custom paths, project name and port. Resume
uses the original database plan and does not redownload or regenerate secrets.
See [interrupted installation](#if-installation-stops) below.

The release also includes
[`unirouter-retained-images-source-notices.tar.gz`](https://github.com/foigent/unirouter/releases/download/v1.14.0-preview.12/unirouter-retained-images-source-notices.tar.gz)
and `SHA256SUMS`. The companion maps all six retained backend/frontend versions
(1.12, 1.13 and 1.14) to version-specific source and notices; the signed current
package contains the shared source materials. Third-party terms remain applicable.

## Check, then install a directly prepared package

Replace the paths below with those supplied for the reviewed release. The
directory must contain `release.tar.gz` and its prepared `payload/` files.
The verifier must support the `manifest_sha256` result introduced with this
installer; an older verifier is rejected before any installation write.

```sh
cd /path/to/verified-release
sudo python3 payload/deploy/install.py check \
  --verifier /path/to/trusted/uniroute-upgrade \
  --public-keys /path/to/trusted/public-keys.json

sudo python3 payload/deploy/install.py install \
  --verifier /path/to/trusted/uniroute-upgrade \
  --public-keys /path/to/trusted/public-keys.json
```

`check` does not create configuration, secrets, containers or databases.
`install` creates `/opt/unirouter`, pulls only the six signed image digests and
starts the database services. It then initializes UR, registers the exact
release and sets a generated administrator password before starting HTTP.
Passwords are not printed in progress output or passed in command arguments.

Use `--directory`, `--name` and `--port` to choose another private directory,
Compose project name or loopback port. Keep these arguments for `resume`.
Installation refuses existing projects; this is not an upgrade command.
Run only one installer for a project at a time, even if using different
directories. Concurrent installation into the same project is unsupported.

## Open the console

The completion message gives the URL and password-file location. On your own
computer, keep an SSH tunnel to the server open:

```sh
ssh -N -L 3000:127.0.0.1:3000 your-user@your-server
```

Open `http://127.0.0.1:3000` on that computer. On the server, privately read the
generated password with `sudo cat /opt/unirouter/admin-password` and sign in as
`admin`. Do not paste this file into an issue or shared log. Complete the
first-customer setup through the console, then follow
[model configuration and the first request](getting-started.md#prepare-model-access).

Installation itself performs no login, provider-key import, qualification
probe or paid model call. A working setup console does not yet mean a model is
ready for application traffic. Keep the loopback boundary while completing
the release-matched setup and model qualification. Public HTTPS exposure and
the final canonical routing cutover follow the
[deployment runbook](../ops/deployment-runbook.md).

## If installation stops

Keep the installation directory, database volumes and original release.
Repeat the command with `resume` in place of `install`, using the same paths,
name, port and Docker daemon. Resume preserves secrets and uses the database's
original installation plan. It checks release registration before repeating
any registration write.

If the administrator-password operation has an uncertain result, resume stops
for maintainer readback. Repeating that operation would create another audit
entry and break the existing first-customer requirement. Do not delete audits,
regenerate secrets or reset the database to get past the error. A failure during
initial file preparation, before `install-state.json` exists, also needs
inspection; it is not an automatically resumable database installation.

For help, share the product version, stopped step and sanitized installer
error. Do not upload `.env`, `admin-password`, a database dump or raw child
command output. This installer has no automatic uninstall or data cleanup.

Maintainer details and outstanding acceptance are in the
[installer contract](../ops/COMMUNITY-INSTALLER.md).
