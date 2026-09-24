# PKG Sender for Synology

[中文说明](README.zh-CN.md)

A Synology NAS / DSM SPK focused fork of [`Loopayeh/pkg-sender`](https://github.com/Loopayeh/pkg-sender).

This fork packages a lightweight Go-based NAS service that lets a PS5 running `pkg-receiver.elf` pull PKG files directly from a Synology NAS over the local network. It is intended for trusted LAN use.

## Current release

Current validated Synology build: `0.1.0-0021`.

Validated hardware/software target:

- Synology DSM 7.2.2
- DS1517+ / x86_64
- PS5 receiver endpoint: `http://<ps5-ip>:12800/api/install`

## Features

- Native Synology DSM package (`PKGSenderNAS`).
- DSM app entry that opens the Web UI.
- Web UI for browsing PKG libraries and installing packages.
- Configurable PS5 IP from the Web UI.
- Configurable PKG library paths from the Web UI, including multiple NAS directories.
- Recursive PKG scanning with stable opaque package IDs.
- HTTP file serving with Range and HEAD support.
- PS5 PKG metadata parsing for title, localized titles, Title ID, Content ID, version, and package type.
- Game family grouping by Title ID.
- Local title alias import/export workflow for manual title curation.
- Persistent install history and FIFO queue with retry, cancel, and reorder controls.
- UDP discovery status display for the PS5 receiver.
- Chinese / English Web UI language switch.

## How it works

1. Load `pkg-receiver.elf` on the PS5 through your normal PS5 WebKit/exploit flow.
2. The receiver listens on port `12800`.
3. Run the Synology SPK on the NAS.
4. Configure the PS5 IP and one or more PKG library paths in the Web UI.
5. Click Install. The NAS sends an install request to the PS5 receiver, and the PS5 pulls the PKG from the NAS over HTTP.

The NAS does not send or load the PS5 payload. Payload loading is outside this SPK's scope.

## Docker

A multi-architecture Docker image is published to GitHub Container Registry. See [docs/docker.md](docs/docker.md).

```sh
docker pull ghcr.io/mrggbond/pkg-sender-synology:latest
```

## Build

The Synology package lives under `nas/spk`.

```sh
cd nas
ARCH=x86_64 GO_BIN=/path/to/go ./spk/build.sh
```

The output is written to:

```text
nas/spk/dist/PKGSenderNAS-<version>-x86_64.spk
```

ARMv8 build target:

```sh
cd nas
ARCH=armv8 GO_BIN=/path/to/go ./spk/build.sh
```

## Configuration

The native SPK stores runtime configuration under the DSM package app-data directory:

```text
/var/packages/PKGSenderNAS/var/config.env
```

Important settings:

```sh
PKGSENDER_PUBLIC_BASE_URL="http://<nas-ip>:9898"
PKGSENDER_PS5_IP="<ps5-ip>"
PKGSENDER_PS5_PORT="12800"
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/More/PKG"]'
```

`PKGSENDER_PACKAGE_DIRS` is preferred when present. `PKGSENDER_PACKAGE_DIR` remains supported for older single-library deployments.

## Documentation

Detailed NAS implementation notes are in [`nas/README.md`](nas/README.md). SPK build and DSM migration notes are in [`nas/spk/README.md`](nas/spk/README.md).

## Relationship to upstream

This repository is a fork of `Loopayeh/pkg-sender`, which is licensed under the MIT License.

This Synology-focused implementation is independent and is not affiliated with, sponsored by, or endorsed by the upstream author. Upstream attribution is preserved in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) and the repository [`LICENSE`](LICENSE).

## License

MIT. See [`LICENSE`](LICENSE).
