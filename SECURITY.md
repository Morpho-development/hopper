# Security Policy

## Supported versions

Security fixes are provided for the latest tagged release. The `main` branch may contain unreleased changes.

## Reporting a vulnerability

Do not open a public issue for a vulnerability. Use GitHub private vulnerability reporting for this repository. Include affected version, reproduction steps, impact, and any suggested remediation.

## Deployment requirements

- Never expose the backend container or SQLite/storage volumes directly.
- Public instances must use HTTPS. Plain HTTP is suitable only for loopback or a trusted encrypted VPN.
- Generate a unique `JWT_ACCESS_SECRET` of at least 32 random characters.
- Keep registration disabled unless additional accounts are intentionally allowed.
- Back up both the database and file-storage volumes together.
- Protect Docker and SSH administration with keys, firewall rules, and timely security updates.

## Data model

- Passwords use bcrypt hashes.
- Refresh and public-share tokens are stored only as SHA-256 hashes.
- Uploaded files use UUID storage names and mode `0600`; original names remain metadata.
- Access tokens stay in browser memory. Refresh tokens use HttpOnly, Secure, SameSite=Strict cookies under HTTPS.
- HTML, SVG and unknown file formats are never rendered by the preview endpoint.

Server administrators can read Docker volumes and database contents. Hopper does not provide encryption against a malicious or compromised root administrator; use encrypted disks and client-side encryption when that threat is in scope.
