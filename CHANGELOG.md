# Changelog

All notable changes are documented here. This project follows Semantic Versioning.

## [Unreleased]

### Added

- Self-hosted Go/SQLite file API and React PWA
- Secure login, session rotation, credential changes and logout
- Files, folders, safe previews, quotas, purge and temporary links
- Cross-device notes and WebSocket events
- Docker Compose local mode and Caddy auto-TLS production mode

### Security

- UUID disk names, path traversal defenses and strict ownership checks
- HttpOnly Secure refresh cookies and memory-only access tokens
- CSP, HSTS, nosniff and same-origin WebSocket checks
- Private backend network and mandatory random JWT secret
