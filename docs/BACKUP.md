# Backup and restore

Hopper stores metadata and sessions in `hopper_hopper-db` and file contents in `hopper_hopper-data`. Back up and restore both volumes as one consistent set.

## Backup

Stop writes briefly, create archives, and restart:

```bash
mkdir -p backups
docker compose stop backend
docker run --rm -v hopper_hopper-db:/source:ro -v "$PWD/backups:/backup" alpine:3.22 tar czf /backup/hopper-db.tar.gz -C /source .
docker run --rm -v hopper_hopper-data:/source:ro -v "$PWD/backups:/backup" alpine:3.22 tar czf /backup/hopper-data.tar.gz -C /source .
docker compose start backend
```

Store archives outside the server and encrypt them before uploading to third-party storage.

## Restore

Restoring replaces current data. Stop the stack and verify archive paths before continuing:

```bash
docker compose down
docker volume create hopper_hopper-db
docker volume create hopper_hopper-data
docker run --rm -v hopper_hopper-db:/target -v "$PWD/backups:/backup:ro" alpine:3.22 sh -c 'rm -rf /target/* && tar xzf /backup/hopper-db.tar.gz -C /target'
docker run --rm -v hopper_hopper-data:/target -v "$PWD/backups:/backup:ro" alpine:3.22 sh -c 'rm -rf /target/* && tar xzf /backup/hopper-data.tar.gz -C /target'
docker compose -f docker-compose.yml -f docker-compose.prod.yml up -d
```

After restore, verify login, storage usage, one download, and `docker compose ps` health.
