# Hopper

[English](README.md) | [Русский](README.ru.md)

Hopper is a self-hosted private file cloud for moving files between computers and phones. It combines a Go API, React PWA, SQLite metadata, local Docker volumes, and optional automatic HTTPS through Caddy.

## Features

- Login-only accounts with bcrypt password hashing
- Rotating HttpOnly, Secure, SameSite session cookies and server-side logout
- Upload, download, folders, rename, safe previews, quotas, and protected full cleanup
- Temporary public links with expiration and download limits
- Notes synchronized between devices
- Responsive installable PWA
- Local Docker mode and public automatic HTTPS mode

## Requirements

- Linux server or computer with Docker Engine and Docker Compose v2
- At least 1 GB RAM and enough disk space for the configured quota
- Public deployment: a public IPv4 address and reachable TCP ports `80` and `443`

## Quick Start

```bash
git clone https://github.com/Morpho-development/hopper.git
cd hopper
cp .env.example .env
openssl rand -hex 48
```

Open `.env` and set the generated value as `JWT_ACCESS_SECRET`. Also set a unique administrator login and a strong password of at least 12 characters:

```env
JWT_ACCESS_SECRET=replace_with_the_generated_secret
FIRST_RUN_ADMIN_LOGIN=admin
FIRST_RUN_ADMIN_PASSWORD=replace_with_a_long_random_password
```

Never commit `.env` or publish its contents.

## Public HTTPS Installation

Point a domain to the server. If you do not own a domain, an address such as `203.0.113.10` can use the free hostname `203-0-113-10.sslip.io`.

Set these values in `.env`:

```env
APP_ENV=production
APP_DOMAIN=203-0-113-10.sslip.io
FRONTEND_BIND=127.0.0.1
```

Start Hopper:

```bash
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

Open `https://203-0-113-10.sslip.io`. Caddy obtains and renews the TLS certificate automatically. The backend is not published on the host, and port `3000` remains loopback-only.

## Local Installation

Keep development mode and the loopback binding in `.env`:

```env
APP_ENV=development
APP_DOMAIN=localhost
FRONTEND_BIND=127.0.0.1
```

Then run:

```bash
docker compose up -d --build
```

Open <http://localhost:3000>. To access Hopper from trusted devices on the same LAN, set `FRONTEND_BIND=0.0.0.0` and restrict port `3000` with the host firewall. Do not expose this plain HTTP mode to the internet.

## First Login

On an empty database, the backend creates the account configured by `FIRST_RUN_ADMIN_LOGIN` and `FIRST_RUN_ADMIN_PASSWORD`. The password can later be changed in Account settings. Existing installations are not reset when these environment values change.

Self-registration is disabled by default:

```env
REGISTRATION_ENABLED=false
```

## Storage Limits

Limits are specified in bytes:

```env
# Total instance quota: 100 GiB
INSTANCE_QUOTA_BYTES=107374182400

# Maximum size of one file: 5 GiB
MAX_UPLOAD_BYTES=5368709120
```

Common quota values:

| Capacity | Bytes |
| --- | ---: |
| 10 GiB | `10737418240` |
| 50 GiB | `53687091200` |
| 100 GiB | `107374182400` |
| 1 TiB | `1099511627776` |

Hopper displays used and available quota in the interface. The host must still have enough real disk space.

## Daily Commands

```bash
# Container status
docker compose -f docker-compose.yml -f docker-compose.prod.yml ps

# Recent logs
docker compose -f docker-compose.yml -f docker-compose.prod.yml logs --tail=100

# Stop without deleting data
docker compose -f docker-compose.yml -f docker-compose.prod.yml down
```

Do not add `-v` to `docker compose down`: it deletes the database and stored files.

## Backup and Update

Back up both Docker volumes before every update. Follow [Backup and restore](docs/BACKUP.md), then run:

```bash
git pull --ff-only
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d --build
```

Database migrations run automatically during backend startup.

## Security

- Use HTTPS for every internet-facing installation.
- Keep registration disabled unless it is explicitly required.
- Use a unique administrator password and protect SSH with keys where possible.
- Keep the OS and Docker updated and store encrypted backups outside the server.
- Public share links grant access to their file until they expire or reach their download limit.

See [SECURITY.md](SECURITY.md) for reporting vulnerabilities and deployment boundaries.

## Development

```bash
cd backend
go test ./...

cd ../frontend
npm ci
npm run build
```

## License

Hopper is licensed under the GNU Affero General Public License v3.0. See [LICENSE](LICENSE).
