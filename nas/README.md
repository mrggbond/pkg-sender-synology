# Synology PKG Sender MVP

This directory is a standalone Synology-focused sender. It intentionally does not depend on the Avalonia/.NET desktop app.

MVP scope:

1. recursively scan a mounted directory for `*.pkg`;
2. expose each scanned PKG at `GET|HEAD /pkg/{id}`;
3. use Go's `http.ServeContent` for byte-range/HEAD support;
4. call the PS5 receiver at `POST http://<ps5>:12800/api/install`;
5. let the PS5 pull the PKG directly from the NAS.

Not included yet: UDP discovery, persistent queue/history, image/folder copy, PS4/GoldHEN support.

## API

- `GET /health`
- `GET /ui/`
- `GET /api/packages`
- `GET /api/families`
- `GET /api/transfers`
- `POST /api/rescan`
- `POST /api/install/{id}`
- `GET|HEAD /pkg/{id}`
- `GET|HEAD /icon/{id}`

The package API returns stable SHA-256 IDs derived from relative paths. Absolute NAS paths are never accepted from HTTP requests.

## PKG metadata

Scanning performs bounded random-access reads of PS5 FIH/CNT metadata instead of reading the full PKG. When available, `GET /api/packages` includes `title`, `titleId`, `contentId`, `version`, `masterVersion`, `targetVersion`, `applicationCategoryType`, `packageType`, and `packageTypeSource`.

Metadata parsing is best-effort. A malformed, encrypted, or unsupported metadata layout leaves `metadataParsed=false` but does not remove the file from the library or block installation.

Package type values are `game`, `patch`, `dlc`, `app`, or `unknown`. Patch classification uses structural/target-version signals. DLC classification may currently use the upstream-compatible title/content-id/filename heuristic and is explicitly marked with `packageTypeSource=heuristic`.

## Game families

`GET /api/families` groups the current scan by normalized `titleId`. A family contains its title, aggregate size, package count, and the concrete packages that can still be installed independently by package ID.

Within a family, packages are ordered as Game → Patch → DLC → App → Unknown; patch versions are ordered newest first. The family title prefers the parsed Game title, so DLC or patch labels do not replace the base game's display name.

Packages without a Title ID are never dropped or combined arbitrarily: each receives its own fallback family keyed by its opaque package ID. Family grouping is derived in memory from the current scan and does not add a database.

## Local covers

`GET|HEAD /icon/{id}` reads a bounded PNG icon directly from the selected PKG. The reader accepts only unencrypted exact icon entries (`0x1200`) or icon variants (`0x1201–0x1220`), validates the PNG signature, and caps a single icon read at 8 MiB.

The family UI uses the base Game package icon as its cover. It intentionally does not promote patch/DLC icons to the family cover because those packages may contain generic or unrelated artwork. Missing icons fall back to a local placeholder and never affect scanning or installation.

## Web UI

Open `http://NAS_IP:9898/ui/` in a browser. The embedded UI has no third-party runtime dependencies and provides:

- Title-ID family grouping with nested Game/Patch/DLC package rows;
- PKG metadata listing and filtering;
- manual library rescan;
- an Install action with confirmation;
- live in-memory transfer status and byte-accurate percentage polling once per second.

The UI uses the existing same-origin JSON API and does not add a second listening port.

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

## Native DSM package

A native DSM 7 SPK build is available under `spk/`. It packages the same Go server as a static Linux binary, runs under DSM's package identity, keeps configuration in the package app-data directory, and does not require Container Manager. See `spk/README.md` for build and migration details.

The Docker and native package variants both use port 9898 by default. Do not start both at the same time.

Real-hardware native-package migration has passed on DSM 7.2.2 / DS1517+ with SPK `0.1.0-0002`: the service runs under the DSM package identity, survives a package restart, scans the existing 5-PKG library, serves covers, and preserves byte-range behavior. The previous Docker container is retained in stopped state as a rollback path.

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

A successful NAS-side response means the receiver accepted the install request. Real-hardware MVP acceptance has passed: the PS5 accepted the request, pulled the PKG from the Synology NAS, completed installation, and the installed game launched successfully.

## Transfer logging

Each completed `/pkg/{id}` request emits one transfer log line with the client IP, requested byte range, HTTP status, and actual response-body bytes sent:

```text
pkg transfer: method=GET client=192.168.32.100 id=... file="Game.pkg" range="bytes=0-1048575" status=206 bytes=1048576
```

`HEAD` requests report `bytes=0`. A PS5 install can issue multiple range requests, so these entries are per HTTP request rather than a cumulative install-progress value.

With Synology Docker bridge port mapping, the container may see the bridge gateway (for example `172.19.0.1`) instead of the original PS5 LAN address. Transfer progress therefore does not rely on the logged client IP.

## Transfer progress

`POST /api/install/{id}` creates or resets an in-memory transfer session for that package. Successful `GET /pkg/{id}` responses are merged as byte intervals, so duplicate, overlapping, retried, or concurrent Range requests do not inflate progress.

`GET /api/transfers` returns the current sessions, for example:

```json
[
  {
    "id": "...",
    "name": "Game.pkg",
    "relativePath": "Game.pkg",
    "status": "downloading",
    "transferred": 34800000000,
    "total": 83129328266,
    "percent": 41.8613,
    "rangeCount": 7,
    "startedAt": "2026-09-22T17:20:00Z",
    "updatedAt": "2026-09-22T17:22:10Z"
  }
]
```

Statuses are `requesting`, `queued`, `downloading`, `complete`, or `error`. `complete` means the HTTP byte coverage reached the PKG size; it does not independently prove that the PS5 finished installing or launching the title.

Transfer sessions are intentionally memory-only in this stage and reset when the container restarts.

## Local development

This module has no third-party Go dependencies.

```sh
go test ./...
go build ./cmd/pkg-sender-nas
```
