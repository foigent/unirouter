# Install on a Linux server

[简体中文](install-linux.zh-CN.md) · [First model request](getting-started.md)

**Preview: this is an unreleased installer candidate; a public download is not
available yet.** Exact Linux/Docker test results and their limits are recorded
in the [native acceptance record](../ops/COMMUNITY-INSTALLER.md#accepted-native-bootstrap-result--2026-10-11).
The commands below are for a prepared, verified release. Cloning this repository
does not supply its signed package, trusted verifier or published images.

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

- Python 3.10 or later and Bash;
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

## Download and prepare an official release

The 1.11.0 source candidate adds [a community bootstrap](../../deploy/community/README.md).
Once a release is published, its reviewed, commit-pinned instructions must provide
the bootstrap script, JSON and independently verified hashes. The script then
downloads and checks the exact verifier, public ring and package before preparing
files or running the signed installer. No official values are published yet.

Run the bootstrap with `install` to combine preparation and installation, or
use `prepare` followed by `check` and `install`. Use the same root identity and
a root-controlled preparation directory throughout. Keep that directory and
the original bootstrap/hash for `resume`; checks and resume do not redownload.
Preparation does not start services or create installation credentials.
The [bootstrap instructions](../../deploy/community/README.md#actions) describe
these arguments and distinguish the release directory from runtime data.

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
