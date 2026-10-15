# Synology native SPK packaging (DSM 6 / DSM 7)

This directory builds native Synology packages for the Go sender, independently of Container Manager. Building an SPK does not stop, replace, publish, or reconfigure a running Docker service.

## Release and compatibility

Latest package release: **[v0.1.1](https://github.com/mrggbond/pkg-sender-synology/releases/tag/v0.1.1)**.

| Package | DSM version | Architecture | Startup behavior |
| --- | --- | --- | --- |
| `PKGSenderNAS-0.1.1-x86_64.spk` | DSM 7 | x86_64 / Intel-AMD | Configure `config.env` before starting |
| `PKGSenderNAS-0.1.1-armv8.spk` | DSM 7 | armv8 / ARM64 | Configure `config.env` before starting |
| `PKGSenderNAS-0.1.1-dsm6-x86_64.spk` | DSM 6 | x86_64 / Intel-AMD | Can start before PS5/library configuration |

The DSM 7.2.2 / DS1517+ x86_64 platform was the historical real-hardware target. **v0.1.1 has passed local tests and package/asset inspection, but no v0.1.1 installation or DSM launcher hardware acceptance has been recorded.** DSM 6 and DSM 7 armv8 are not hardware-validated.

DSM 7 installs with `conf/privilege` (`run-as: package`); DSM 6 uses a separate legacy SPK layout and startup script. Package Center icon resources are 64×64 (DSM 7) or 72×72 (DSM 6), with a 256×256 icon also bundled. Both editions include 16/24/32/48/64/72/256px application-menu icons. Their `ui/config` uses DSM's `.url` launcher definition targeting `http://<NAS-IP>:9898/ui/`.

## Build

Run from `nas/spk/` with Go **1.24 or newer**:

```sh
# DSM 7 x86_64 (default)
GO_BIN=/path/to/go ./build.sh

# DSM 7 armv8
ARCH=armv8 GO_BIN=/path/to/go ./build.sh

# DSM 6 x86_64
GO_BIN=/path/to/go ./build-dsm6.sh

# Independent Web UI regression tests
node --test ../internal/httpserver/ui.test.cjs
```

Build scripts default to version `0.1.1` and write packages into `dist/`. They run Go tests, cross-build static Linux executables, build the SPK archive, and run their respective `verify.sh` or `verify-dsm6.sh` checks. The packaging process does **not** publish Docker images or GitHub Releases.

## Installation and initial configuration

Install the SPK matching the DSM version and NAS CPU architecture using **Package Center → Manual Install**. Both editions store editable settings in:

```text
/var/packages/PKGSenderNAS/var/config.env
```

**DSM 7:** its current lifecycle script deliberately avoids launching the daemon until you replace the `CHANGE_ME_*` placeholders, set a readable existing library directory, and start the package. An example configuration:

```sh
PKGSENDER_PACKAGE_DIR="/volume1/PS5/PKG"
# Optional multi-root override:
# PKGSENDER_PACKAGE_DIRS='["/volume1/PS5/PKG","/volume1/PS5/MorePKG"]'
PKGSENDER_LISTEN=":9898"
PKGSENDER_PUBLIC_BASE_URL="http://192.168.1.20:9898"
PKGSENDER_PS5_IP="192.168.1.50"
PKGSENDER_PS5_PORT="12800"
```

**DSM 6:** the friendly `dsm6/config.env.example` defaults to `PKGSENDER_PS5_IP=""`, `PKGSENDER_PUBLIC_BASE_URL=""` and `PKGSENDER_PACKAGE_DIR="/var/packages/PKGSenderNAS/var/packages"`. The startup script creates the default library directory when needed, so you can start the service, open `http://<NAS-IP>:9898/ui/`, and use **Save PS5 IP** / **PKG Library paths** to enter your own settings. When the public base URL is empty, the daemon builds package download URLs using the HTTP request Host; access it through a LAN address reachable by the PS5.

After the daemon starts, both editions can update PS5 IP and PKG library paths from the Web UI. `PKGSENDER_PACKAGE_DIRS` (JSON array) takes precedence over `PKGSENDER_PACKAGE_DIR`. You must still grant the appropriate read/traverse permissions on your shared folders. The PS5 must separately run `pkg-receiver.elf`; the NAS package does **not** send/load this payload.

The DSM menu icon and direct URL both point to port **9898**. Do not run a Docker copy and the native package on the same TCP port. An older installation may require an SPK upgrade/reinstall and refreshing the DSM browser session to pick up changed launcher resources; the shortcut's actual appearance still requires device-side verification.

## Shared-folder permission

DSM 7 runs under the configured package identity, not root. In DSM Shared Folder permissions, grant the system-internal user associated with **PS5 PKG Sender / PKGSenderNAS** read/traverse access to every directory configured by `PKGSENDER_PACKAGE_DIR` or `PKGSENDER_PACKAGE_DIRS`. Check effective access on DSM 6 as well; its package layout and execution account rules differ from DSM 7. Write permission to the PKG library is not required.

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

`verify.sh` (DSM 7) and `verify-dsm6.sh` (DSM 6) are the pre-install structure gates. They check SPK layout, lifecycle-script syntax, metadata, checksums, payload executable mode, the DSM `.url` application shortcut, and all seven application-menu icon assets. DSM 7 additionally validates its privilege/resource declarations; DSM 6 additionally checks the friendly startup defaults and the 72px Package Center icon. The separate Node UI suite verifies DLC folding/search, PS5-only online/offline status, and bilingual labels.

These checks are **not substitutes for installing the release on real DSM hardware**. Historical DSM 7.2.2 / DS1517+ smoke tests showed that the earlier x86_64 binary executed on Linux and correctly rejected missing required configuration. The current DSM 6 package intentionally allows missing PS5 IP during initial startup.

DSM 7.2.2 also exposes `synopkg query <spk>`, but it is not used as a blocking validation gate here. On the test NAS it returns the same generic failure for a temporary SPK reconstructed from an already-installed custom package, so that result is not specific enough to distinguish a malformed hand-built SPK.


## Real-hardware migration acceptance

The following results are **historical acceptance records for earlier DSM 7.2.2 / DS1517+ (`x86_64`, `avoton`) builds through `0.1.0-0020`**, not hardware acceptance for the v0.1.1 application shortcut, DSM 6 compatibility, or ARM64:

- upgrades preserved `config.env` byte-for-byte and restarted cleanly; the test NAS was running `0.1.0-0020` when this acceptance record was captured;
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
