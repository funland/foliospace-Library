# FolioSpace Library Docker Compose Quick Start

[SpatialEMU beginner path](docs/spatialemu-quickstart.md) · [简体中文](docs/spatialemu-quickstart.zh-CN.md)

This package runs FolioSpace Library 1.10 from the published Linux AMD64/ARM64 image. It works with Docker Desktop on Mac or Windows, Docker Engine with Compose on Linux, or a compatible NAS. A NAS and coding are not required.

Movie/TV management is included. Optional TMDB matching is disabled by default
and requires your own API Read Access Token, not a shared project key. Follow
the [setup and licensing guide](docs/tmdb-setup.md) · [中文](docs/tmdb-setup.zh-CN.md).

FolioSpace is optional for SpatialEMU: supported local files can be opened directly without this server. The SpatialEMU macOS app is a client, not a FolioSpace server.

## 1. Prepare the files

In the folder containing `docker-compose.yml` and `.env.example`, run the commands for your host.

macOS or Linux:

```sh
cp .env.example .env
mkdir -p data/config data/library data/books data/games data/videos
```

Windows PowerShell:

```powershell
Copy-Item .env.example .env
New-Item -ItemType Directory -Force data/config, data/library, data/books, data/games, data/videos
```

The default relative folders are ready for a first test. Put user-owned files in the matching `data/*` folder. To index folders that already exist elsewhere, edit `.env` and set the matching host path.

`FOLIOSPACE_CONFIG_PATH` must point to writable persistent storage. Media mounts are read-only. Leave `FOLIOSPACE_API_TOKEN` empty for the beginner path so the web setup page can create the first token.

Example custom paths:

```dotenv
FOLIOSPACE_CONFIG_PATH=/path/to/foliospace-library/config
FOLIOSPACE_LIBRARY_PATH=/path/to/Media
FOLIOSPACE_BOOKS_PATH=/path/to/Books
FOLIOSPACE_GAMES_PATH=/path/to/Games
FOLIOSPACE_VIDEOS_PATH=/path/to/Videos
```

Synology uses the same variables; for example, `/volume1/docker/foliospace-library/config` can be the config path.

## 2. Start the service

```sh
docker compose up -d
docker compose ps
```

The published Compose default is port `8080`. Open `http://localhost:8080` on the Docker host or `http://<host-LAN-IP>:8080` from another device on the same trusted network.

In the first-run web page:

1. Create an access token with at least 8 characters and store it safely.
2. Choose a container path such as `/library`, `/books`, `/games`, or `/videos`.
3. Finish setup. Advanced game-catalog options can stay at their defaults for the first connection.

The picker uses container paths, not the original Mac, Windows, Linux, or NAS host path.

## 3. Connect SpatialEMU

Open SpatialEMU's FolioSpace connection screen, enter the full service URL and the same token, select **Connect**, and confirm that the expected catalog loads.

- [SpatialEMU connection setup guide](https://spatialemu.com/guides/foliospace-connection/) · [简体中文](https://spatialemu.com/zh-cn/guides/foliospace-connection/)
- [Learn about FolioSpace](https://spatialemu.com/foliospace/) · [简体中文](https://spatialemu.com/zh-cn/foliospace/)

## 4. Update

Back up the directory configured by `FOLIOSPACE_CONFIG_PATH`, then run:

```sh
docker compose pull
docker compose up -d
```

## 5. Stop the service

```sh
docker compose down
```

This removes the container but does not delete bind-mounted configuration or media files.

## Security and content boundary

Do not expose port `8080` directly to the public internet. Prefer a trusted LAN, VPN such as Tailscale, or an HTTPS reverse proxy with appropriate access controls.

FolioSpace and SpatialEMU do not include, provide, or download games, ROMs, BIOS files, firmware, or third-party content catalogs. Users must provide files they have the right to use.

## Troubleshooting

- `docker compose logs -f foliospace-library` shows startup and scan errors.
- If setup cannot write data, verify ownership and permissions on `FOLIOSPACE_CONFIG_PATH`.
- If a folder is missing from the picker, verify its host path in `.env`, then recreate the container with `docker compose up -d`.
- Change `FOLIOSPACE_PORT` if port `8080` is already in use, and enter that actual port in SpatialEMU.
- Change `FOLIOSPACE_CONTAINER_NAME` when running more than one test instance.
