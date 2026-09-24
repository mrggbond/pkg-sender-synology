# Docker image

[中文说明](docker.zh-CN.md)

The Docker image packages the same Go-based NAS service used by the Synology SPK. It is useful on generic Linux servers, Container Manager, Portainer, Unraid, TrueNAS SCALE, or any other Docker-capable host on the same LAN as the PS5.

## Image

```text
ghcr.io/mrggbond/pkg-sender-synology
```

Published platforms:

```text
linux/amd64
linux/arm64
```

## Quick start

```sh
docker run -d \
  --name pkg-sender-nas \
  --restart unless-stopped \
  -p 9898:9898 \
  -v /volume1/PS5/PKG:/packages:ro \
  -e PKGSENDER_PACKAGE_DIR=/packages \
  -e PKGSENDER_LISTEN=:9898 \
  -e PKGSENDER_PUBLIC_BASE_URL=http://192.168.1.20:9898 \
  -e PKGSENDER_PS5_IP=192.168.1.50 \
  -e PKGSENDER_PS5_PORT=12800 \
  ghcr.io/mrggbond/pkg-sender-synology:latest
```

Open:

```text
http://192.168.1.20:9898/ui/
```

Replace:

- `/volume1/PS5/PKG` with the host directory that contains PKG files.
- `192.168.1.20` with the NAS/server LAN IP reachable from the PS5.
- `192.168.1.50` with the PS5 LAN IP running `pkg-receiver.elf`.

## Docker Compose

```yaml
services:
  pkg-sender-nas:
    image: ghcr.io/mrggbond/pkg-sender-synology:latest
    container_name: pkg-sender-nas
    restart: unless-stopped
    read_only: true
    ports:
      - "9898:9898"
    volumes:
      - "/volume1/PS5/PKG:/packages:ro"
    environment:
      PKGSENDER_PACKAGE_DIR: /packages
      PKGSENDER_LISTEN: ":9898"
      PKGSENDER_PUBLIC_BASE_URL: "http://192.168.1.20:9898"
      PKGSENDER_PS5_IP: "192.168.1.50"
      PKGSENDER_PS5_PORT: "12800"
```

## Multiple library paths

Mount multiple host directories into different container paths and set `PKGSENDER_PACKAGE_DIRS` as a JSON array.

```yaml
services:
  pkg-sender-nas:
    image: ghcr.io/mrggbond/pkg-sender-synology:latest
    container_name: pkg-sender-nas
    restart: unless-stopped
    read_only: true
    ports:
      - "9898:9898"
    volumes:
      - "/volume1/PS5/Base:/library/base:ro"
      - "/volume1/PS5/Updates:/library/updates:ro"
      - "/volume1/PS5/DLC:/library/dlc:ro"
    environment:
      PKGSENDER_PACKAGE_DIRS: '["/library/base","/library/updates","/library/dlc"]'
      PKGSENDER_LISTEN: ":9898"
      PKGSENDER_PUBLIC_BASE_URL: "http://192.168.1.20:9898"
      PKGSENDER_PS5_IP: "192.168.1.50"
      PKGSENDER_PS5_PORT: "12800"
```

`PKGSENDER_PACKAGE_DIRS` takes priority when present. `PKGSENDER_PACKAGE_DIR` remains supported for a single mounted library.

## Payload note

The Docker image does not send or load `pkg-receiver.elf`. Load the ELF on the PS5 through your normal PS5 WebKit/exploit workflow, then use the web UI to send install requests to the receiver on port `12800`.

## Local build

```sh
cd nas
docker build -t pkg-sender-nas:local .
```

Multi-architecture build with Buildx:

```sh
docker buildx build \
  --platform linux/amd64,linux/arm64 \
  -t ghcr.io/mrggbond/pkg-sender-synology:latest \
  --push \
  ./nas
```
