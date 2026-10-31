# Install on a Linux server

**Upgrading an existing M127 or M128 server?** Use the [data-preserving upgrade guide](../ops/COMMUNITY-UPGRADE.md). Keep the fresh-install command for a new instance.

Explicitly non-expiring packages use signed `unirouter.upgrade-manifest.v2` with `expires_at: "never"` (UniRouter 1.14.0+). The standalone verifier and backend must both support v2, including `upgrade record-applied`. Existing finite v1 packages retain their original deadline; replacing or re-signing them cannot resume a plan bound to a different package hash. Every check/install/resume still verifies the original signature, hashes, creation time (with the existing five-minute future tolerance), platform, schema and release sequence. Non-expiring does not authorize rollback, skip trust checks or extend the owner's finite signing-authority window. This policy alone does not establish public availability or a verified deployment.

[简体中文](install-linux.zh-CN.md) · [First model request](getting-started.md)

**Linux amd64 preview: v1.15.3-preview.20, source
`1a0230b50f799e2f9c64089270d525ec1ac34ca7`, M129, release sequence 20.**
The actual signature and all 26 payloads have been verified. The signed predecessor sequence range is 0–19, with minimum schema M127; existing M127/M128 instances use the upgrade guide.
The six release attachments and both UR images are public. [Formal Linux acceptance](https://github.com/foigent/unirouter/actions/runs/41196358700) passed the complete public signed download, six anonymous image pulls, M129 installation, login, first-customer initialization and interruption recovery.
[Source PR #486](https://github.com/foigent/unirouter/pull/486) separately passed [disposable Linux installation and ordinary-upgrade cases](https://github.com/foigent/unirouter/actions/runs/41166284675), covering M127/M128→M129 and later M129→M129 with synthetic data and temporary trust.
These results do not claim an upgrade of your existing server or real Provider/UR→XI acceptance. Run the original-host checks before maintenance. See [release status](release-readiness.md).
This entry installs a new instance. Retain prior attempts and use unused paths and a new project; resume cannot switch package hashes. Historical versions and failed candidates remain preserved; sequence 14 is not installable or reused. Cloning the repository does not supply signed release assets.

The installer prepares PostgreSQL, Redis, UniRouter and its monitoring services,
generates the required local secrets, initializes the database and opens a
private management console. You do not need Go, Node.js or npm to compile UR.
After installation, use the console to configure your organization and model.
X-Insight is optional and is not required to install UniRouter.

## Before you start

Use a new installation directory and a new Docker Compose project. This preview
supports Linux amd64. The existing direct prepared-
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
[preview release](https://github.com/foigent/unirouter/releases/tag/v1.15.3-preview.20)
before executing downloaded code. The script is pinned to the product source;
the JSON pins the verifier, public ring and signed package. A checksum beside
an untrusted download does not independently establish trust. This entry does
not replace an existing public ring or require a private signing key.

| Download | Bytes | SHA-256 |
| --- | ---: | --- |
| `bootstrap.py` at source 1a0230b5 | 25,086 | `499e6e018c13709eef883e672ff381256cbe5c9f7e7b896a30b64a091e33be05` |
| `community-bootstrap.json` | 1,130 | `7628632bf1d4039985481ce79bfd1fae2eaefa6144b2f39b343c818816df35d2` |
| `uniroute-upgrade-linux-amd64` | 4,117,964 | `d767e64f2db31f97c886c53e14fe30b948794933aef28137db982442a2a0b02f` |
| `public-keys.json` | 83 | `96c91635460ddfb2a14aa1ea97d8445951730e9eb04a33cbdf4320141e61d63d` |
| `unirouter-1.15.3-linux-amd64.tar.gz` | 130,052,464 | `d3a6b16abd7561a19ca9b7e35c5e8aaa7fa018a2fb1f221c1a0dc30826c8726b` |

Open a root shell with `sudo -i`, then run
the block below. It requires unused download, preparation and runtime paths;
keep them for recovery. A download error or a checksum failure must stop before
the installer executes.

```sh
set -eu
umask 077
mkdir -m 700 /opt/unirouter-downloads-1.15.3
cd /opt/unirouter-downloads-1.15.3
curl --fail --location --proto '=https' --proto-redir '=https' \
  --output bootstrap.py \
  https://raw.githubusercontent.com/foigent/unirouter/1a0230b50f799e2f9c64089270d525ec1ac34ca7/deploy/community/bootstrap.py
curl --fail --location --proto '=https' --proto-redir '=https' \
  --output community-bootstrap.json \
  https://github.com/foigent/unirouter/releases/download/v1.15.3-preview.20/community-bootstrap.json
printf '%s  %s\n' \
  499e6e018c13709eef883e672ff381256cbe5c9f7e7b896a30b64a091e33be05 bootstrap.py \
  7628632bf1d4039985481ce79bfd1fae2eaefa6144b2f39b343c818816df35d2 community-bootstrap.json \
  | sha256sum --check --status
python3 -I -B bootstrap.py install \
  --bootstrap community-bootstrap.json \
  --bootstrap-sha256 7628632bf1d4039985481ce79bfd1fae2eaefa6144b2f39b343c818816df35d2 \
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
[`unirouter-retained-images-source-notices.tar.gz`](https://github.com/foigent/unirouter/releases/download/v1.15.3-preview.20/unirouter-retained-images-source-notices.tar.gz)
and `SHA256SUMS`. The unchanged historical companion retains the 1.12/1.13
image mappings and shared-material references. Retained 1.14.0 materials remain
in [Preview 13](https://github.com/foigent/unirouter/releases/tag/v1.14.0-preview.13).
The current signed package contains 1.15.3 source-disposition, component notices
and required source materials. Third-party terms remain applicable.

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

## Choose your administrator password

Installation generates an initial private password; it does not prompt for a
password of your choice. After installation succeeds, you can choose one
through the shipped native command on the server. For the default installation:

```sh
sudo docker compose --project-name unirouter \
  --project-directory /opt/unirouter/runtime \
  --env-file /opt/unirouter/runtime/.env \
  -f /opt/unirouter/runtime/docker-compose.yml \
  -f /opt/unirouter/runtime/installation.json \
  -f /opt/unirouter/runtime/runtime-image.json \
  exec backend ./uniroute admin set-password --username admin --org-id 1
```

Use your actual project name and installation directory if customized. Run in
an interactive terminal: the command prompts twice without echo. Password
values belong in that private prompt, not command arguments or shared logs.
A successful change invalidates existing administrator sessions. The original
`admin-password` file still contains the generated old value; use the password
you just chose for subsequent logins and store it in your password manager.

## Open the console

The completion message gives the URL and password-file location. On your own
computer, keep an SSH tunnel to the server open:

```sh
ssh -N -L 3000:127.0.0.1:3000 your-user@your-server
```

Open `http://127.0.0.1:3000` on that computer and sign in as `admin` with the
password you chose above. If you kept the generated initial password, privately
read it on the server with `sudo cat /opt/unirouter/admin-password`. Do not paste
a password or this file into an issue or shared log. Complete the
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
