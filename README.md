# PKG Sender for Synology

[中文说明](README.zh-CN.md)

A Synology NAS / DSM SPK focused fork of [`Loopayeh/pkg-sender`](https://github.com/Loopayeh/pkg-sender).

This fork packages a lightweight Go-based NAS service that lets a PS5 running `pkg-receiver.elf` pull PKG files directly from a Synology NAS over the local network. It is intended for trusted LAN use.

## Current release

Latest GitHub Release: **[v0.1.3](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.3)**.

Download the SPK matching **both** the DSM version and NAS CPU architecture:

| Release asset | Target |
| --- | --- |
| `PKGSenderNAS-0.1.3-x86_64.spk` | DSM 7, x86_64 (Intel/AMD) |
| `PKGSenderNAS-0.1.3-armv8.spk` | DSM 7, armv8 (ARM64) |
| `PKGSenderNAS-0.1.3-dsm6-x86_64.spk` | DSM 6, x86_64 (Intel/AMD) |
| `pkg-receiver.elf` | Optional PS5 receiver payload; load it separately on the PS5 |

All three v0.1.3 SPKs passed local Go tests, package build and archive checks. **On-device v0.1.3 installation and DSM launcher validation have not yet been confirmed.** Historical physical-NAS acceptance was performed on DSM 7.2.2 / DS1517+ (x86_64) with earlier builds. DSM 6 and DSM 7 ARM64 remain hardware-unverified.

## What's new in v0.1.3

- **Unknown-type PKGs now collapse with DLC** by default. The existing Show/Hide DLC button counts both types while expanded entries retain their original **Unknown** classification.
- Searching reveals matching DLC and Unknown files; expanded groups remain independent by game and keep their state through automatic refresh.
- DSM 6 and DSM 7 packages include the same bilingual behavior.

## Improvements carried over from previous releases

- DLC rows start **collapsed per game** with independent Show/Hide controls. Expanded state persists across automatic refreshes, and searches expose matching DLC files even when their group is collapsed.
- The connectivity area shows **PS5 online/offline only** (green/red); the NAS status indicator was removed. PS5 IP entry and saving remain available.
- Fixed the DSM application-menu shortcut configuration and included correctly sized launcher icons. The shortcut opens `http://<NAS-IP>:9898/ui/`.
- Both DSM 6 and DSM 7 SPKs now start with safe, package-owned defaults, allowing first-time setup from the Web UI without manually editing `config.env`. DSM 7 upgrades preserve existing settings; legacy placeholders are handled at runtime.

## Features

- Native Synology SPKs for DSM 6 x86_64 and DSM 7 x86_64/armv8, without requiring Container Manager.
- DSM application-menu shortcut to the browser-based Web UI, plus direct access at `http://<NAS-IP>:9898/ui/`.
- Web UI configuration of PS5 IP and multiple PKG library directories.
- Game families grouped by Title ID; Game, Patch and App rows remain visible while DLC and Unknown-type files start collapsed.
- Recursive PKG scanning, HTTP Range/HEAD serving, and metadata parsing (title, localized titles, IDs, version and Game/Patch/DLC classification).
- Local title-alias import/export for manual title curation.
- Persistent installation history, sequential FIFO queue, Retry, queued-only Cancel, and queued Up/Down controls.
- UDP receiver-beacon discovery, with **only the configured PS5's online/offline state** shown in the UI.
- English/Chinese language switch.

## Install and first use

1. Download the correct SPK from the [v0.1.3 release](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.3) and install it manually in DSM Package Center.
2. **DSM 6:** the friendly package starts with an empty PS5 IP and a package-owned default PKG directory. Open `http://<NAS-IP>:9898/ui/`, set **PS5 IP** and **PKG Library paths**, then save. Its public package URL is derived from the incoming HTTP request host when not explicitly configured.
3. **DSM 7:** starting with v0.1.2, the package also starts before configuration. Open the DSM app shortcut or `http://<NAS-IP>:9898/ui/`, then save **PS5 IP** and **PKG Library paths** in the Web UI. You no longer need to edit `config.env` during a fresh install. Upgrades preserve existing configuration; old `CHANGE_ME_*` values are handled in memory only. A custom inaccessible library still requires fixing its path or permissions.
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

Packages are created in `nas/spk/dist/`, with `0.1.3` as the default package version. SPK packaging is separate from Docker image publishing.

## Configuration

Native SPKs keep their editable configuration at `/var/packages/PKGSenderNAS/var/config.env`. **Both DSM 6 and DSM 7 v0.1.2** start with a package-owned empty library and blank PS5 IP, allowing setup in the Web UI after starting. To configure a fixed public URL or edit advanced settings manually, use:

```sh
PKGSENDER_PUBLIC_BASE_URL="http://192.168.1.20:9898"
PKGSENDER_PS5_IP="192.168.1.50"
PKGSENDER_PS5_PORT="12800"
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
# Optional multi-root override:
# PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/More/PKG"]'
```

The default public base URL is empty, so the NAS derives package URLs from the incoming HTTP request Host. Open the Web UI through a LAN address reachable from the PS5. When set, `PKGSENDER_PACKAGE_DIRS` takes precedence over the legacy single-root `PKGSENDER_PACKAGE_DIR`. Shared-folder access still depends on Synology permissions; upgrading preserves existing configured paths and other settings.

## Documentation

Detailed NAS implementation notes are in [`nas/README.md`](nas/README.md). SPK build and DSM migration notes are in [`nas/spk/README.md`](nas/spk/README.md).

## Relationship to upstream

This repository is a fork of `Loopayeh/pkg-sender`, which is licensed under the MIT License.

This Synology-focused implementation is independent and is not affiliated with, sponsored by, or endorsed by the upstream author. Upstream attribution is preserved in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md) and the repository [`LICENSE`](LICENSE).

## License

MIT. See [`LICENSE`](LICENSE).
