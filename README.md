# PKG Sender for Synology

[中文说明](README.zh-CN.md)

A Synology NAS / DSM SPK focused fork of [`Loopayeh/pkg-sender`](https://github.com/Loopayeh/pkg-sender).

This fork packages a lightweight Go-based NAS service that lets a PS5 running `pkg-receiver.elf` pull PKG files directly from a Synology NAS over the local network. It is intended for trusted LAN use.

## Current release

Latest GitHub Release: **[v0.1.1](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.1)**.

Download the SPK matching **both** the DSM version and NAS CPU architecture:

| Release asset | Target |
| --- | --- |
| `PKGSenderNAS-0.1.1-x86_64.spk` | DSM 7, x86_64 (Intel/AMD) |
| `PKGSenderNAS-0.1.1-armv8.spk` | DSM 7, armv8 (ARM64) |
| `PKGSenderNAS-0.1.1-dsm6-x86_64.spk` | DSM 6, x86_64 (Intel/AMD) |
| `pkg-receiver.elf` | Optional PS5 receiver payload; load it separately on the PS5 |

All three v0.1.1 SPKs passed local Go tests, package build and archive checks. **On-device v0.1.1 installation and DSM launcher validation have not yet been confirmed.** Historical physical-NAS acceptance was performed on DSM 7.2.2 / DS1517+ (x86_64) with earlier builds. DSM 6 and DSM 7 ARM64 remain hardware-unverified.

## What's new in v0.1.1

- DLC rows start **collapsed per game** with independent Show/Hide controls. Expanded state persists across automatic refreshes, and searches expose matching DLC files even when their group is collapsed.
- The connectivity area shows **PS5 online/offline only** (green/red); the NAS status indicator was removed. PS5 IP entry and saving remain available.
- Fixed the DSM application-menu shortcut configuration and included correctly sized launcher icons. The shortcut opens `http://<NAS-IP>:9898/ui/`.
- Added a DSM 6 x86_64 SPK with startup-before-configuration support and a DSM 6-sized Package Center icon.

## Features

- Native Synology SPKs for DSM 6 x86_64 and DSM 7 x86_64/armv8, without requiring Container Manager.
- DSM application-menu shortcut to the browser-based Web UI, plus direct access at `http://<NAS-IP>:9898/ui/`.
- Web UI configuration of PS5 IP and multiple PKG library directories.
- Game families grouped by Title ID; Game and Patch rows remain visible while DLC starts collapsed.
- Recursive PKG scanning, HTTP Range/HEAD serving, and metadata parsing (title, localized titles, IDs, version and Game/Patch/DLC classification).
- Local title-alias import/export for manual title curation.
- Persistent installation history, sequential FIFO queue, Retry, queued-only Cancel, and queued Up/Down controls.
- UDP receiver-beacon discovery, with **only the configured PS5's online/offline state** shown in the UI.
- English/Chinese language switch.

## Install and first use

1. Download the correct SPK from the [v0.1.1 release](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.1) and install it manually in DSM Package Center.
2. **DSM 6:** the friendly package starts with an empty PS5 IP and a package-owned default PKG directory. Open `http://<NAS-IP>:9898/ui/`, set **PS5 IP** and **PKG Library paths**, then save. Its public package URL is derived from the incoming HTTP request host when not explicitly configured.
3. **DSM 7:** the current package **does not start its daemon before initial configuration**. Edit `/var/packages/PKGSenderNAS/var/config.env` to replace `CHANGE_ME_NAS_IP` and `CHANGE_ME_PS5_IP` and set an existing readable PKG directory; then start the package. After startup, PS5 IP and library paths can be changed in the Web UI.
4. Load `pkg-receiver.elf` on the PS5 through your normal PS5 WebKit/exploit flow. The receiver listens on TCP `12800`; the NAS listens for its UDP beacon on `12801`.
5. Open the DSM application menu shortcut or `http://<NAS-IP>:9898/ui/` and click **Install** on a selected PKG. The PS5 pulls the PKG from the NAS over HTTP.

The NAS **does not send or load the PS5 payload**. Loading the ELF is a separate PS5-side step. Use this service on a trusted LAN. Grant the package's system-internal user read/traverse permission to configured shared folders. **Do not run the native SPK and a Docker instance on the same TCP port 9898 simultaneously.**

For configuration details and upgrade notes, see [native SPK instructions](nas/spk/README.md).

## Docker

A multi-architecture Docker image is published to GitHub Container Registry. See [docs/docker.md](docs/docker.md).

```sh
docker pull ghcr.io/mrggbond/pkg-sender-synology:latest
```

## Build

The native SPK build scripts live in `nas/spk/` and require Go 1.24 or newer. From the repository root:

```sh
# DSM 7 x86_64
GO_BIN=/path/to/go ./nas/spk/build.sh

# DSM 7 ARM64
ARCH=armv8 GO_BIN=/path/to/go ./nas/spk/build.sh

# DSM 6 x86_64
GO_BIN=/path/to/go ./nas/spk/build-dsm6.sh

# Standalone browser UI tests
node --test nas/internal/httpserver/ui.test.cjs
```

Packages are created in `nas/spk/dist/`, with `0.1.1` as the default package version. SPK packaging is separate from Docker image publishing.

## Configuration

Native SPKs keep their editable configuration at `/var/packages/PKGSenderNAS/var/config.env`. DSM 7 needs valid initial `PKGSENDER_PUBLIC_BASE_URL`, `PKGSENDER_PS5_IP`, and `PKGSENDER_PACKAGE_DIR` values **before its daemon starts**. Example:

```sh
PKGSENDER_PUBLIC_BASE_URL="http://192.168.1.20:9898"
PKGSENDER_PS5_IP="192.168.1.50"
PKGSENDER_PS5_PORT="12800"
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
# Optional multi-root override:
# PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/More/PKG"]'
```

DSM 6 instead ships with an empty PS5 IP, empty public base URL and a package-owned default library directory, so its service can start before Web UI configuration. When set, `PKGSENDER_PACKAGE_DIRS` takes precedence over the legacy single-root `PKGSENDER_PACKAGE_DIR`. Shared-folder access still depends on Synology permissions.

## Documentation

Detailed NAS implementation notes are in [`nas/README.md`](nas/README.md). SPK build and DSM migration notes are in [`nas/spk/README.md`](nas/spk/README.md).

## Relationship to upstream

This repository is a fork of `Loopayeh/pkg-sender`, which is licensed under the MIT License.

This Synology-focused implementation is independent and is not affiliated with, sponsored by, or endorsed by the upstream author. Upstream attribution is preserved in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) and the repository [`LICENSE`](LICENSE).

## License

MIT. See [`LICENSE`](LICENSE).
