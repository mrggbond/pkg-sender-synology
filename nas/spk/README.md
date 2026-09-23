# Synology native SPK packaging

This directory builds a native DSM 7 package for the Go sender. It is separate from the Container Manager deployment: building or querying an SPK does not stop, replace, or reconfigure the running Docker service.

## Current target

The first real-hardware target is DSM 7.2.2 on DS1517+ (`x86_64`, Synology platform `avoton`). The build script also supports `armv8` for later ARM64 validation.

The package runs with DSM's package identity via `conf/privilege` (`run-as: package`). It does not require root privileges after installation.

Package Center icons are derived from the repository's existing `payload/icon0.png` branding and committed as the DSM-required 64×64 `PACKAGE_ICON.PNG` and 256×256 `PACKAGE_ICON_256.PNG`.

## Build

From `nas/spk`:

```sh
GO_BIN=/path/to/go ./build.sh
```

Defaults:

- version: `0.1.0-0021`
- architecture: `x86_64`
- output: `dist/PKGSenderNAS-0.1.0-0021-x86_64.spk`

ARM64 package:

```sh
ARCH=armv8 GO_BIN=/path/to/go ./build.sh
```

The build runs the native Go test suite first, then cross-builds a static Linux binary, assembles an xz-compressed `package.tgz`, creates the plain-tar SPK archive with extended attributes disabled, and runs `verify.sh`.

## Persistent configuration

DSM provides `/var/packages/PKGSenderNAS/var` as the package app-data area. On first install, `postinst` creates:

```text
/var/packages/PKGSenderNAS/var/config.env
```

from the bundled example. Replace the two `CHANGE_ME_*` values and set the PKG library path before starting:

```sh
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
# Optional multi-root override:
# PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/PS5/MorePKG"]'
PKGSENDER_LISTEN=":9898"
PKGSENDER_PUBLIC_BASE_URL="http://192.168.32.5:9898"
PKGSENDER_PS5_IP="192.168.32.100"
PKGSENDER_PS5_PORT="12800"
```

If the config is missing or still contains `CHANGE_ME`, the lifecycle script reports that configuration is required and deliberately does not launch the daemon. This prevents an initial package install from failing solely because the NAS/PS5 addresses have not been entered yet.

## Shared-folder permission

The native process runs as the package identity rather than root. In DSM Shared Folder permissions, grant the system-internal user associated with **PS5 PKG Sender / PKGSenderNAS** read/traverse access to every directory configured by `PKGSENDER_PACKAGE_DIR` or `PKGSENDER_PACKAGE_DIRS`. Write permission is not required.

On DSM installations where the shared folder uses Synology ACLs, Unix mode bits alone may not be sufficient. Grant the package identity read/traverse access to the library path and its existing descendants. The DS1517+ migration target required an explicit `PKGSenderNAS` read/traverse ACE because the shared-folder ACL otherwise denied the package user even though the directory mode was permissive.

## Runtime files

Persistent package data:

```text
/var/packages/PKGSenderNAS/var/
├── aliases.json
├── config.env
├── history.json
├── pkg-sender-nas.log
└── pkg-sender-nas.pid
```

The lifecycle script exports `PKGSENDER_HISTORY_FILE=${VAR_DIR}/history.json` automatically. Users do not need to add this setting to `config.env`. The daemon keeps at most 100 recent install records and writes the file with mode `0600` using atomic replacement. Upgrades preserve the package app-data directory, so `history.json` survives SPK upgrades and restarts.

The lifecycle script also exports `PKGSENDER_TITLE_ALIASES_FILE=${VAR_DIR}/aliases.json` automatically. Missing `aliases.json` is valid and simply means no manual aliases are applied. If present, this file must be manually curated JSON; the daemon never searches the network for titles.

Because the native package always configures persistent queue storage, failure to open or decode `history.json` disables new Install/Retry queue writes with HTTP 503 while leaving browsing, health, discovery, metadata, covers, and Range serving operational. The daemon does not silently downgrade a broken configured store to a volatile memory-only queue.

Immutable installed payload:

```text
/var/packages/PKGSenderNAS/target/
├── bin/pkg-sender-nas
├── share/config.env.example
└── var/PKGSenderNAS.sc
```

The package intentionally does not declare `adminport` or DSM port ownership for TCP 9898. On the migration target, WebStation already has the existing Docker-project service registered against `127.0.0.1:9898`; declaring the same TCP port again makes DSM reject the SPK with error 283. The native daemon still binds the configured `PKGSENDER_LISTEN` address (default `:9898`) once the Docker service is stopped.

`conf/resource` registers only UDP `12801` through `var/PKGSenderNAS.sc`, so DSM can account for the PS5 receiver beacon listener without taking ownership of TCP 9898. The protocol entry uses `port_forward="no"`, keeping this LAN-only discovery service out of DSM's built-in port-forwarding rule selection.

## Safe DSM validation

`verify.sh` is the primary pre-install gate. It checks the SPK layout, lifecycle script syntax, required DSM metadata, embedded icon fields, payload contents, executable mode, UDP `12801` discovery declaration, absence of duplicate TCP `9898` ownership, and the MD5 recorded for `package.tgz`.

For hardware validation without installation, copy the SPK to the NAS, unpack it into a temporary directory, and execute the extracted binary with an empty environment. On the DS1517+ test target the x86_64 binary executes and fails closed on the missing `PKGSENDER_PS5_IP`, which proves the packaged Linux binary is runnable without binding a port.

DSM 7.2.2 also exposes `synopkg query <spk>`, but it is not used as a blocking validation gate here. On the test NAS it returns the same generic failure for a temporary SPK reconstructed from an already-installed custom package, so that result is not specific enough to distinguish a malformed hand-built SPK.


## Real-hardware migration acceptance

DSM 7.2.2 on DS1517+ (`x86_64`, `avoton`) has passed the native-package, discovery, persistent-history, FIFO-queue, persistence-failure, queued-cancellation, queued-reordering, localized-title, Japanese-fallback, local-alias, alias-export, alias-import, bilingual-UI, configurable PS5 target, DSM UI-entry, and payload-sender-removal gates through `0.1.0-0020`:

- upgrades preserve `config.env` byte-for-byte and restart cleanly; production currently runs `0.1.0-0020`;
- the daemon runs as `PKGSenderNAS:PKGSenderNAS`, not root, and the package identity can read the existing PS5 PKG library through Synology ACLs;
- DSM registers only `12801/udp` for discovery; the configured PS5 at `192.168.32.100` remains beacon-online;
- `/health` reports all 5 PKGs and HTTP Range remains `206 Partial Content` after both queue-stage upgrades;
- production history remains `[]`, the Web UI serves `/api/retry/`, `queueStatus`, and Retry controls, and the former Docker container remains stopped as a rollback path;
- the complete FIFO behavior was exercised with the `0007` binary on a localhost-only sender at `127.0.0.1:19989` and fake receiver at `127.0.0.1:19990`: first task `active`, second `queued`, and only one receiver POST;
- restarting that sender converted the accepted active task to `queueStatus=interrupted` without replay while the still-queued task automatically resumed and became active;
- manual Retry created a new record with `retryOf`, stayed queued behind the active task, and was submitted only after a complete HTTP GET changed the active task to `complete` and released the FIFO slot;
- queue/history persistence was owned by `PKGSenderNAS:PKGSenderNAS` with mode `0600`, and all temporary FIFO-test files/processes/listeners were removed;
- final `0008` added fail-closed behavior for a configured-but-unavailable persistence store. A separate localhost-only test used an intentionally corrupt `0600` history file: health/package listing and Range remained operational, `POST /api/install/{id}` returned HTTP 503, the fake receiver received zero POSTs, and the corrupt file SHA-256 remained unchanged;
- `0009` additionally rolls back in-memory queue state when a required persistence update fails, preventing the worker from releasing the next FIFO slot on a disk error; the Web UI displays persisted queued items with their FIFO position; the production upgrade preserved config byte-for-byte and passed health, discovery, empty-history, Range 206, UI-contract, and Docker-rollback checks;
- `0010` adds persisted queued-only cancellation: a localhost-only hardware gate queued A then B, observed A `active` / B `queued` with exactly one fake-receiver POST, cancelled B with HTTP 200 and no extra POST, rejected cancellation of active A with HTTP 409, then verified after sender restart that A became `interrupted`, B remained `cancelled`, and the receiver POST count was still one; the cancellation history file remained package-owned mode `0600` and all test artifacts were removed;
- `0011` adds persisted `queueOrder`, queued-only Up/Down reordering, backward migration for older queued records without an order, and Web UI queue controls; an isolated hardware gate observed A `active` with B/C `queued`, moved C above B without an extra receiver POST, completed A's full 10-byte Range transfer, then verified the second receiver install request was `C.pkg`, with C `active` and B still `queued`; submitting/active records remained immovable, the history file was `PKGSenderNAS:PKGSenderNAS` mode `0600`, and all temporary test artifacts/listeners were removed;
- `0012` adds English primary and Chinese secondary title display from PKG `localizedParameters`; production metadata shows `Crimson Desert` with secondary title `赤血沙漠`;
- `0013` adds Japanese subtitle fallback, offline local alias fallback from `aliases.json`, and `GET /api/title-alias-missing` for manually curating aliases; production shows Japanese subtitle fallback where available and does not use network lookup;
- `0014` adds `GET /api/title-alias-export`, an editable alias template with `_aiPrompt`, and a Web UI button to copy an AI prompt for manual simplified-Chinese alias lookup;
- `0015` adds `POST /api/title-alias-import` and a Web UI paste/import dialog. Import accepts AI-returned JSON or the exported alias template, ignores `_aiPrompt`, atomically writes `aliases.json`, merges with existing non-empty aliases, reloads the alias table, and immediately rescans the library;
- all temporary fail-closed test files/processes/listeners were removed and production remained healthy throughout.

No real PS5 install was triggered during the queue/fail-closed gates; the earlier MVP real-install acceptance remains the installation-path baseline.

Do **not** install or start the native SPK while the stable Docker service is still bound to port 9898. Installation/start testing is a separate migration gate after explicitly stopping the Docker instance.
