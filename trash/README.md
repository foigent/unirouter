# Local Trash

This directory is a quarantine area for local, non-product artifacts that should not be used by the application or by release packaging.

Current contents may include Playwright CLI snapshots, screenshots, and console logs moved from `.playwright-cli/` during local UI investigation.

Rules:

- Do not put source code, product docs, migrations, release binaries, or required assets here.
- Do not bulk-delete this directory. If cleanup is needed, delete or move one explicit path at a time according to the project agent instructions.
- Contents are ignored by Git except this README.
