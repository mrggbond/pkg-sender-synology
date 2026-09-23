# Synology PKG Sender MVP

This directory is a standalone Synology-focused sender. It intentionally does not depend on the Avalonia/.NET desktop app.

MVP scope:

1. recursively scan a mounted directory for `*.pkg`;
2. expose each scanned PKG at `GET|HEAD /pkg/{id}`;
3. use Go's `http.ServeContent` for byte-range/HEAD support;
4. call the PS5 receiver at `POST http://<ps5>:12800/api/install`;
5. let the PS5 pull the PKG directly from the NAS.

Not included yet: automatic retry, image/folder copy, PS4/GoldHEN support.

## API

- `GET /health`
- `GET /ui/`
- `GET /api/packages`
- `GET /api/families`
- `GET /api/transfers`
- `GET /api/history`
- `GET /api/discovery`
- `POST /api/rescan`
- `POST /api/install/{id}`
- `POST /api/retry/{historyId}`
- `POST /api/cancel/{historyId}`
- `POST /api/reorder/{historyId}`
- `GET|HEAD /pkg/{id}`
- `GET|HEAD /icon/{id}`

The package API returns stable SHA-256 IDs derived from relative paths. Absolute NAS paths are never accepted from HTTP requests.

## PKG metadata

Scanning performs bounded random-access reads of PS5 FIH/CNT metadata instead of reading the full PKG. When available, `GET /api/packages` includes `title`, `displayTitle`, `secondaryTitle`, `localizedTitles`, `titleId`, `contentId`, `version`, `masterVersion`, `targetVersion`, `applicationCategoryType`, `packageType`, and `packageTypeSource`.

Metadata parsing is best-effort. A malformed, encrypted, or unsupported metadata layout leaves `metadataParsed=false` but does not remove the file from the library or block installation.

Localized titles are read from the PKG's own `localizedParameters`; they are not machine translated. `displayTitle` prefers an English title when present. `secondaryTitle` uses the first available value in this order: PKG Chinese title → PKG Japanese title → local alias table. It is omitted when it would duplicate the primary display title.

Package type values are `game`, `patch`, `dlc`, `app`, or `unknown`. Patch classification uses structural/target-version signals. DLC classification may currently use the upstream-compatible title/content-id/filename heuristic and is explicitly marked with `packageTypeSource=heuristic`.

## Local title aliases

The daemon can load an optional local alias file from `PKGSENDER_TITLE_ALIASES_FILE`. Native SPK sets this automatically to its package app-data path `aliases.json`; missing files are treated as an empty alias table. The service never searches the network or writes aliases automatically.

Alias keys are normalized `titleId` or `contentId` values. Values are localized title strings. Aliases are used only when the PKG itself has no Chinese or Japanese subtitle.

```json
{
  "_aiPrompt": "Copy GET /api/title-alias-export aiPrompt here; the daemon ignores this field.",
  "PPSA07862": {
    "zh-Hans": "怪物猎人：荒野",
    "zh-Hant": "魔物獵人 荒野"
  },
  "PPSA32802": {
    "zh-Hans": "零 ～红蝶～ REMAKE",
    "zh-Hant": "零 ～紅蝶～ REMAKE"
  }
}
```

Export titles that still need aliases:

```sh
curl http://NAS_IP:9898/api/title-alias-missing
```

Export a complete manual-curation package with `missing`, editable `aliasTemplate`, and an `aiPrompt` that can be copied to an AI assistant to look up simplified Chinese names by Title ID:

```sh
curl http://NAS_IP:9898/api/title-alias-export
```

After the AI returns JSON, paste it into the Web UI via **Import aliases**, or import it through the API. Import writes `aliases.json` atomically, preserves existing non-empty aliases, reloads the alias table, and rescans the library immediately:

```sh
curl -X POST http://NAS_IP:9898/api/title-alias-import \
  -H 'Content-Type: application/json' \
  --data-binary @aliases.json
```

## Game families

`GET /api/families` groups the current scan by normalized `titleId`. A family contains its title, aggregate size, package count, and the concrete packages that can still be installed independently by package ID.

Within a family, packages are ordered as Game → Patch → DLC → App → Unknown; patch versions are ordered newest first. The family title prefers the parsed Game display title, so DLC or patch labels do not replace the base game's display name. When available, the UI shows English as the primary title and Chinese, then Japanese, then local alias as the secondary title.

Packages without a Title ID are never dropped or combined arbitrarily: each receives its own fallback family keyed by its opaque package ID. Family grouping is derived in memory from the current scan and does not add a database.

## Local covers

`GET|HEAD /icon/{id}` reads a bounded PNG icon directly from the selected PKG. The reader accepts only unencrypted exact icon entries (`0x1200`) or icon variants (`0x1201–0x1220`), validates the PNG signature, and caps a single icon read at 8 MiB.

The family UI uses the base Game package icon as its cover. It intentionally does not promote patch/DLC icons to the family cover because those packages may contain generic or unrelated artwork. Missing icons fall back to a local placeholder and never affect scanning or installation.

## PS5 UDP discovery

The NAS listens on UDP `12801` for the receiver's `PKGSENDER v1` beacon. The receiver broadcasts this every 3 seconds; the NAS records the packet source IP and most recent timestamp.

`GET /api/discovery` returns the configured PS5 target plus any discovered receiver IPs. A receiver is considered online when a beacon was seen within the last 10 seconds. Discovery is informational in this stage: installs still use `PKGSENDER_PS5_IP`, and a listener failure never blocks scanning, Range serving, or installation through the configured target.

The embedded UI reports whether the configured PS5 is currently visible by beacon. It does not silently replace or persist the configured PS5 address.

## Web UI

Open `http://NAS_IP:9898/ui/` in a browser. The embedded UI has no third-party runtime dependencies and provides:

- Title-ID family grouping with nested Game/Patch/DLC package rows;
- PKG metadata listing and filtering, including English primary titles and Chinese/Japanese/local-alias secondary titles when available;
- manual library rescan;
- an Install action with confirmation;
- a persistent FIFO install queue with explicit manual Retry for failed/interrupted attempts and Cancel for records that are still queued;
- live in-memory transfer status and byte-accurate percentage polling once per second.
- recent persistent install/transfer history with receiver, transfer, and install-outcome semantics kept separate.
- passive PS5 receiver discovery status from UDP `12801`.

The UI uses the existing same-origin JSON API and does not add a second listening port.

## Configuration

Required:

- `PKGSENDER_PS5_IP`: PS5 LAN IP running `pkg-receiver.elf`.
- `PKGSENDER_PUBLIC_BASE_URL=http://<NAS-LAN-IP>:9898`: explicit NAS LAN URL that the PS5 can reach.

Optional:

- `PKGSENDER_PACKAGE_DIR` (default `/packages`)
- `PKGSENDER_PACKAGE_DIRS`: optional JSON array for multiple library roots. When set, it takes precedence over `PKGSENDER_PACKAGE_DIR`, for example `["/volume1/PS5/PKG","/volume1/PS5/MorePKG"]`.
- `PKGSENDER_LISTEN` (default `:9898`)
- `PKGSENDER_PS5_PORT` (default `12800`)
- `PKGSENDER_HISTORY_FILE`: JSON persistence path for recent install history and queue state. If unset, the queue is explicitly memory-only. Native SPK sets this automatically to its package app-data directory. If a configured file cannot be opened or decoded, browsing/Range/health remain available but new install/retry requests fail closed with HTTP 503 rather than silently degrading to a volatile queue.
- `PKGSENDER_TITLE_ALIASES_FILE`: optional JSON file for manually curated title aliases. If unset or missing, aliases are empty. Native SPK sets this automatically to its package app-data directory.

## Synology Container Manager

Copy this `nas/` directory to the NAS or build/publish the image elsewhere.

Create a project environment file from `env.example` (name it `.env` on the NAS) and set:

```env
NAS_IP=192.168.1.20
PS5_IP=192.168.1.50
PKG_DIR=/volume1/PS5/PKG
```

Then create the project from `compose.yaml`.

The package directory is mounted read-only. The container does not need privileged mode. The current Docker compose deployment does not expose UDP `12801`; native SPK is the validated discovery target. If Docker discovery is needed later, publish `12801/udp` or use an equivalent LAN-reachable networking mode.

## Native DSM package

A native DSM 7 SPK build is available under `spk/`. It packages the same Go server as a static Linux binary, runs under DSM's package identity, keeps configuration in the package app-data directory, and does not require Container Manager. See `spk/README.md` for build and migration details.

The Docker and native package variants both use port 9898 by default. Do not start both at the same time.

Real-hardware acceptance on DSM 7.2.2 / DS1517+ is current through SPK `0.1.0-0021`. The full FIFO behavior was exercised with the `0007` binary using localhost-only sender/receiver endpoints: first-active/second-queued serialization, restart conversion of an accepted active record to `interrupted` without replay, automatic resume of a still-queued record, explicit Retry creating a new `retryOf` record, FIFO release only after a complete HTTP transfer, and `0600` package-owned persistence all passed. `0008` added corrupt-persistence fail-closed behavior; `0009` added fail-closed in-memory rollback and FIFO position display. `0010` passed queued-only cancellation. `0011` passed an in-place production upgrade and an isolated reordering gate. `0012` added localized title display from PKG metadata. `0013` adds Japanese subtitle fallback plus offline local alias fallback and missing-alias export. `0014` adds an AI-prompt export package and Web UI copy action for manual alias curation. `0015` adds paste-and-import alias JSON with atomic write, merge, reload, and immediate rescan. `0016` adds browser-language based Chinese/English Web UI localization, a manual language switch, simplified package metadata rows, relative-path-only display, and Installed/已安装 reinstall confirmation behavior. `0017` adds configurable PS5 target IP and DSM UI entry. `0018` removes the path label prefix, keeps the language switch at the far right, and makes Installed/已安装 buttons gray while preserving reinstall confirmation behavior. `0020` removes NAS-side payload sending, places the PS5 IP controls in the status row, and makes online/offline state bold with green/red coloring. `0021` adds Web-configurable multi-root PKG library paths via `PKGSENDER_PACKAGE_DIRS` while preserving single-root `PKGSENDER_PACKAGE_DIR` compatibility. Production remains healthy with 5+ PKGs, PS5 beacon status available, Range 206, and the previous Docker container stopped as a rollback path.

## Smoke test

List packages:

```sh
curl http://NAS_IP:9898/api/packages
```

Check PS5 discovery:

```sh
curl http://NAS_IP:9898/api/discovery
```

On validated native SPK hardware, the configured receiver appears with `configured=true` and `online=true` while `pkg-receiver.elf` is broadcasting.

Check recent install history:

```sh
curl http://NAS_IP:9898/api/history
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

Enqueue installation for the PS5:

```sh
curl -X POST http://NAS_IP:9898/api/install/ID
```

`POST /api/install/{id}` returns `202 Accepted` after the NAS has accepted the request into its queue. When `PKGSENDER_HISTORY_FILE` is configured and healthy (the native-SPK default), the enqueue is persisted before the response; when the setting is intentionally omitted, the queue is memory-only. A configured-but-unavailable persistence store returns `503 Service Unavailable` and never contacts the receiver. A 202 does not mean the receiver has accepted the request yet. Receiver submission and HTTP transfer progress appear asynchronously in `GET /api/history`. The earlier real-hardware MVP acceptance remains valid: the PS5 accepted a request, pulled the PKG from the Synology NAS, completed installation, and the installed game launched successfully.

## Transfer logging

Each completed `/pkg/{id}` request emits one transfer log line with the client IP, requested byte range, HTTP status, and actual response-body bytes sent:

```text
pkg transfer: method=GET client=192.168.32.100 id=... file="Game.pkg" range="bytes=0-1048575" status=206 bytes=1048576
```

`HEAD` requests report `bytes=0`. A PS5 install can issue multiple range requests, so these entries are per HTTP request rather than a cumulative install-progress value.

With Synology Docker bridge port mapping, the container may see the bridge gateway (for example `172.19.0.1`) instead of the original PS5 LAN address. Transfer progress therefore does not rely on the logged client IP.

## Transfer progress

When the queue worker begins submitting a task to the receiver, it creates or resets the in-memory transfer session for that package. Successful `GET /pkg/{id}` responses are merged as byte intervals, so duplicate, overlapping, retried, or concurrent Range requests do not inflate progress.

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

Live transfer sessions remain memory-only and reset when the process restarts. Persistent install history is a separate record stream described below; it does not recreate an in-flight byte-range tracker after restart.

## Persistent install history

`GET /api/history` returns up to the 100 most recent install attempts. Each `POST /api/install/{id}` creates a distinct history ID, so reinstalling the same PKG does not overwrite the previous record.

History intentionally separates four states:

- `queueStatus`: `queued`, `submitting`, `active`, `complete`, `error`, `interrupted`, or `cancelled` for the NAS-side FIFO queue;
- `controlStatus`: `pending`, `requesting`, `accepted`, `error`, or `interrupted` for the NAS → receiver control request;
- `transferStatus`: `waiting`, `downloading`, `complete`, `not_started`, or `interrupted` for the PS5 → NAS HTTP transfer;
- `installStatus`: currently always `unverified`, because the receiver does not provide a reliable final install-completion callback to the NAS.

`transferStatus=complete` therefore means the observed HTTP byte coverage reached the PKG size; it does **not** mean the PS5 installation or game launch was independently verified.

The queue is strictly sequential. Each queued record has a persisted `queueOrder`; the worker submits the queued record with the lowest order, then waits until that package reaches `transferStatus=complete` before submitting the next record. A receiver control failure releases the queue slot immediately. There is no automatic retry. `startedAt` remains an audit timestamp and is never rewritten to reorder the queue.

On restart, records that were still `queued` remain queued and are resumed only after the HTTP listener is bound. Records that were already `submitting` or `active` become `queueStatus=interrupted` and are never automatically replayed, because the NAS cannot prove whether the PS5 accepted or partially processed the earlier request. `POST /api/retry/{historyId}` is explicit user intent and creates a new history record with `retryOf` pointing to the previous attempt.

`POST /api/cancel/{historyId}` is intentionally narrower than Retry: it only succeeds while `queueStatus=queued`. Cancellation is persisted as `queueStatus=cancelled` and releases that package from the pending set. Once a task is `submitting` or `active`, cancellation returns HTTP 409 because the receiver may already have accepted or started processing it; the NAS does not claim to remotely cancel PS5 work already in flight.

`POST /api/reorder/{historyId}` accepts `{"direction":"up"}` or `{"direction":"down"}` and only moves records that are still `queued`. Moving a `submitting`/`active` record, or moving beyond a queue boundary, returns HTTP 409. Older persisted queue records without `queueOrder` are migrated on load using their original FIFO `startedAt`/ID order.

When `PKGSENDER_HISTORY_FILE` is configured, history and queue state are stored together in a versioned JSON file using same-directory temporary-file write, `fsync`, and atomic rename. Enqueue and the transition to `submitting` are persisted before any receiver network request is made. Progress writes are throttled to the first observed progress, at most once every five seconds, and immediately on transfer completion. Queue/control terminal-state changes are written immediately. Retention never drops `queued`, `submitting`, or `active` records even if the normal 100-record history limit is exceeded.

If the configured persistence file is corrupt, unreadable, or otherwise unavailable at startup, the service does not overwrite it and does not silently switch the install queue to memory-only. Package listing, metadata, covers, health, discovery, and Range serving continue; Install, Retry, Cancel, and Reorder mutations fail closed with HTTP 503 until persistence is repaired.

## Local development

This module has no third-party Go dependencies.

```sh
go test ./...
go build ./cmd/pkg-sender-nas
```
