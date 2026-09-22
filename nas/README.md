# Synology PKG Sender MVP

This directory is a standalone Synology-focused sender. It intentionally does not depend on the Avalonia/.NET desktop app.

MVP scope:

1. recursively scan a mounted directory for `*.pkg`;
2. expose each scanned PKG at `GET|HEAD /pkg/{id}`;
3. use Go's `http.ServeContent` for byte-range/HEAD support;
4. call the PS5 receiver at `POST http://<ps5>:12800/api/install`;
5. let the PS5 pull the PKG directly from the NAS.

Not included yet: PKG metadata, covers, Game/Patch/DLC families, UDP discovery, queue UI, image/folder copy, PS4/GoldHEN support.

## API

- `GET /health`
- `GET /api/packages`
- `POST /api/rescan`
- `POST /api/install/{id}`
- `GET|HEAD /pkg/{id}`

The package API returns stable SHA-256 IDs derived from relative paths. Absolute NAS paths are never accepted from HTTP requests.

## Configuration

Required:

- `PKGSENDER_PS5_IP`: PS5 LAN IP running `pkg-receiver.elf`.
- `PKGSENDER_PUBLIC_BASE_URL=http://<NAS-LAN-IP>:9898`: explicit NAS LAN URL that the PS5 can reach.

Optional:

- `PKGSENDER_PACKAGE_DIR` (default `/packages`)
- `PKGSENDER_LISTEN` (default `:9898`)
- `PKGSENDER_PS5_PORT` (default `12800`)

## Synology Container Manager

Copy this `nas/` directory to the NAS or build/publish the image elsewhere.

Create a project environment file from `env.example` (name it `.env` on the NAS) and set:

```env
NAS_IP=192.168.1.20
PS5_IP=192.168.1.50
PKG_DIR=/volume1/PS5/PKG
```

Then create the project from `compose.yaml`.

The package directory is mounted read-only. The container does not need privileged mode. MVP uses ordinary TCP port mapping; host networking is not required until UDP auto-discovery is added.

## Smoke test

List packages:

```sh
curl http://NAS_IP:9898/api/packages
```

Take one `id` and validate HEAD:

```sh
curl -I http://NAS_IP:9898/pkg/ID
```

Validate a byte range:

```sh
curl -v -H 'Range: bytes=0-1023' http://NAS_IP:9898/pkg/ID -o /dev/null
```

Expected response: `206 Partial Content`, `Content-Range`, and `Accept-Ranges: bytes`.

Queue installation on the PS5:

```sh
curl -X POST http://NAS_IP:9898/api/install/ID
```

A successful NAS-side response means the receiver accepted the install request. Final MVP acceptance still requires a real PS5 to request `/pkg/{id}` from the NAS and complete installation.

## Local development

This module has no third-party Go dependencies.

```sh
go test ./...
go build ./cmd/pkg-sender-nas
```
