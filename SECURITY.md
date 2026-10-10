# Security reporting

Do not post vulnerabilities, credentials, access tokens, customer data or
unredacted execution payloads in public issues or pull requests.

## Private reports

When GitHub private vulnerability reporting is enabled for this repository,
use **Security → Advisories → Report a vulnerability**:

https://github.com/foigent/unirouter/security/advisories/new

The repository is being prepared for public release. Enabling and verifying
this reporting channel is a publication prerequisite; its availability has
not yet been confirmed. If the private form is unavailable, open an issue
requesting a confidential reporting channel **without including vulnerability
details**. Do not assume a public issue or an unverified email address is a
private channel.

A useful report identifies the affected version, conditions, impact and a
minimal reproduction using synthetic data. Send only the information needed
to investigate. Remove secrets, real payloads and internal host details.

## Supported versions

No public support window or production certification is established by this
preparation branch. The first public release must state its supported versions
and security update procedure. Historical internal deployment evidence is not
a support commitment for every installation.

Report concerns about access boundaries, routing authorization, financial
integrity, credential handling, execution evidence, installation or update
verification. Maintainers coordinate a fix and disclosure appropriate to the
confirmed impact. No response-time SLA or bounty is implied.
