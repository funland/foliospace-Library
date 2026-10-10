# SpatialEMU + FolioSpace beginner quick start

For the wider Reader / Player / SpatialEMU product family and third-party client integration, see the [Library overview](../README.md#clients-and-downloads) and [Client API quick start](api/client-v1.md#external-client-quick-start).

[简体中文](spatialemu-quickstart.zh-CN.md)

FolioSpace Library is an optional self-hosted catalog for SpatialEMU. SpatialEMU can open supported local files directly without FolioSpace.

You do not need a NAS and you do not need to write code. You do need:

- Docker Desktop on a Mac or Windows PC, Docker Engine with Compose on Linux, or a compatible AMD64/ARM64 NAS with Docker/Compose support;
- one or more folders containing files you own and are allowed to use;
- the FolioSpace service URL; and
- an access token that you create during first-run setup.

The SpatialEMU macOS app is a client, not a FolioSpace server. FolioSpace Library currently runs as the published Linux Docker image; there is no separate native macOS FolioSpace server app.

## 1. Prepare the Compose folder

Download or clone this repository, then open a terminal in the folder containing [`docker-compose.yml`](../docker-compose.yml) and [`.env.example`](../.env.example).

On macOS or Linux:

```sh
cp .env.example .env
mkdir -p data/config data/library data/books data/games data/videos
```

On Windows PowerShell:

```powershell
Copy-Item .env.example .env
New-Item -ItemType Directory -Force data/config, data/library, data/books, data/games, data/videos
```

The default folders are enough for a first test. Put user-owned game files in `data/games`, books in `data/books`, and so on. To use folders that already exist elsewhere, edit the matching `FOLIOSPACE_*_PATH` values in `.env` with a text editor.

Leave `FOLIOSPACE_API_TOKEN` empty for the beginner path. The web setup page will create the first token. The `/config` mount must stay writable and persistent; media mounts are read-only.

## 2. Start FolioSpace Library

```sh
docker compose up -d
docker compose ps
```

The default host port is `8080`. Open:

- `http://localhost:8080` when the browser is on the Docker host; or
- `http://<host-LAN-IP>:8080` from another device on the same trusted network.

## 3. Finish first-run setup

In the FolioSpace web page:

1. Create an access token with at least 8 characters and store it safely.
2. Choose a container path such as `/games`, `/books`, `/library`, or `/videos`.
3. Give the folder a name and choose its media type.
4. Finish setup. Advanced game-catalog options can stay at their defaults for a first connection.

The web page uses container paths. If a host folder is missing, update its bind mount in `.env` and recreate the container with `docker compose up -d`.

## 4. Connect SpatialEMU

If you only want to open a local file, use SpatialEMU's normal import/open flow and stop here.

To use the self-hosted catalog:

1. Open the FolioSpace connection screen in SpatialEMU.
2. Enter the full FolioSpace URL, including `http://` or `https://` and the port when required.
3. Enter the same access token created in the FolioSpace web setup.
4. Select **Connect**, then confirm that the expected catalog entries load before opening a file.

Use the two official help routes for different purposes:

- [SpatialEMU connection setup guide](https://spatialemu.com/guides/foliospace-connection/) — the direct setup flow.
- [Learn about FolioSpace](https://spatialemu.com/foliospace/) — product purpose and responsibility boundaries.

## 5. Safe troubleshooting

```sh
docker compose logs -f foliospace-library
```

- If the browser cannot open FolioSpace, confirm `docker compose ps` reports the service as running and that the client device can reach the Docker host.
- If a folder is missing, verify the corresponding host path in `.env`, then run `docker compose up -d` again.
- If SpatialEMU rejects the connection, verify the complete URL and re-enter the same token. Do not post private URLs or tokens in screenshots or support messages.
- Do not expose port `8080` directly to the public internet. Prefer a trusted LAN, VPN such as Tailscale, or a correctly configured HTTPS reverse proxy.

FolioSpace and SpatialEMU do not include, provide, or download games, ROMs, BIOS files, firmware, or third-party content catalogs. Users must supply files they have the right to use.
