# FolioSpace Library Docker Compose Quick Start

This package runs FolioSpace Library 0.996 on a Docker host or NAS. The image
supports Linux AMD64 and ARM64.

## 1. Prepare the files

```sh
cp .env.example .env
mkdir -p data/config data/library data/books data/games data/videos
```

Edit `.env` and set each host path to the folder you want FolioSpace Library to
index. `/config` must be writable and persistent. Media mounts are read-only.

Synology example:

```dotenv
FOLIOSPACE_CONFIG_PATH=/volume1/docker/foliospace-library/config
FOLIOSPACE_LIBRARY_PATH=/volume1/Media/Comics
FOLIOSPACE_BOOKS_PATH=/volume1/Media/Books
FOLIOSPACE_GAMES_PATH=/volume1/Media/GameROMS
FOLIOSPACE_VIDEOS_PATH=/volume1/Media/Videos
```

## 2. Start the service

```sh
docker compose up -d
docker compose ps
```

Open `http://YOUR-SERVER-IP:8080`. Complete first-run setup and create an access
token. In the library picker, use container paths such as `/library`, `/books`,
`/games`, or `/videos`, not the original NAS paths.

## 3. Update

Back up the directory configured by `FOLIOSPACE_CONFIG_PATH`, then run:

```sh
docker compose pull
docker compose up -d
```

## 4. Stop or remove the container

```sh
docker compose down
```

This does not delete bind-mounted configuration or media files.

## Security

Do not expose port 8080 directly to the public internet. Prefer a trusted LAN,
VPN such as Tailscale, or an HTTPS reverse proxy with appropriate access
controls.

## Troubleshooting

- `docker compose logs -f foliospace-library` shows startup and scan errors.
- If setup cannot write data, verify ownership and permissions on the config
  path.
- If a folder is missing from the picker, verify its host path in `.env`, then
  recreate the container with `docker compose up -d`.
- Change `FOLIOSPACE_PORT` if port 8080 is already in use.
- Change `FOLIOSPACE_CONTAINER_NAME` when running more than one test instance.
