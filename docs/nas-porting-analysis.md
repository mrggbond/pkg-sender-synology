# Synology NAS Porting Analysis (M0)

## Status

- Scope: M0 only — repository audit, porting boundary, baseline validation.
- Upstream: `https://github.com/Loopayeh/pkg-sender`
- Branch: `main`
- Baseline commit: `f879995bd9e3a8bf67bf22cb94152c224db5d732`
- Baseline subject: `Version 1.0.0 (drop droidNN counter; APK named PkgSender-<ver>-android.apk)`
- M0 conclusion: **GO for M1, with a required headless-boundary cleanup before a single-port NAS service.**
- M0 intentionally does not implement the NAS host.

## Post-M0 implementation decision — 2026-09-22

The implementation direction changed after this audit. The target is now **Synology NAS only**, not a shared .NET desktop/NAS host. The active MVP is a standalone Go service under `nas/` with only this path:

`recursive *.pkg scan -> HTTP Range server -> PS5 :12800 /api/install -> PS5 pulls the PKG from the NAS`

The .NET extraction/M1 discussion later in this document remains useful as historical audit context, but it is **not the current implementation plan**. The Go MVP intentionally omits metadata, covers, Game/Patch/DLC family logic, UDP discovery, image/folder copy, PS4/GoldHEN support, and desktop code reuse.


The repository already contains most of the PS5/NAS data-plane functionality needed for a Synology build. The main work is not reimplementing the receiver protocol; it is separating UI-owned library/catalog logic from the existing cross-platform core and removing a small number of desktop/UI assumptions from `LoopDPI.Core`.

## Repository component map

| Component | Current responsibility | NAS relevance |
| --- | --- | --- |
| `LoopDPI.Core/RangeFileServer.cs` | Raw TCP HTTP server, `/catalog`, `/icon/{id}`, `/pkg/{id}`, manifests, GET/HEAD, byte ranges, served-byte counters | Primary reusable PKG data plane |
| `LoopDPI.Core/NetDiscovery.cs` | LAN NIC enumeration, receiver beacon listener on UDP 12801, sender announce on UDP 12802, TCP sweep | Reusable discovery layer |
| `LoopDPI.Core/ConsoleClient.cs` | Receiver/RPI probe, `/api/install`, status, pull/cancel/pause helpers | Primary reusable PS5 install client |
| `LoopDPI.Core/ReceiverClient.cs` | Receiver file API client: stat/mkdir/write/done | Reusable for later image/file-copy features, not required for PKG install MVP |
| `LoopDPI.Core/TransferManager.cs` | Chunked file/folder copy, resume, retries, progress | Later image/file-copy feature; not required for PKG install MVP |
| `LoopDPI.Core/PkgInfo.cs` | PKG metadata model + PS4/PS5 package parser | Parser is reusable; model currently has an Avalonia UI dependency |
| `LoopDPI.Core/GameLibrary.cs` | Folder/image readers, image/container metadata, Python header fallback | Mostly reusable; helper discovery has desktop path assumptions |
| `LoopDPI.Core/AppSettings.cs` | Desktop sender settings and persistence | Current semantics should not be reused as NAS configuration |
| `LoopDPI.Core/UpdateService.cs` | Desktop updater/installer launch | Not part of NAS runtime |
| `library/LibraryScanner.cs` | Recursive library scan and JSON cache | Must move behind a Core/headless service boundary |
| `library/Views/LibraryView.axaml.cs` | UI plus role/family derivation, catalog construction, server lifecycle, queue orchestration | Contains domain logic that must be extracted before NAS parity |
| `library/*.axaml*` | Avalonia desktop UI | Desktop-only |
| `droid/*` | Android app/UI and SAF/direct-source integration | Separate client target, not part of Synology host |
| `payload/main.c` | PS5 receiver implementation and console Web UI | Protocol authority/reference; unchanged by NAS port |
| `tests/sendtest`, `tests/srvtest` | Existing test utilities | Useful baseline/Range regression assets once .NET SDK is available |

## Protocol compatibility findings

### Existing HTTP PKG server is already suitable as the NAS data plane

`LoopDPI.Core/RangeFileServer.cs` is not a Windows-only server. It uses `TcpListener`, `TcpClient`, `NetworkStream`, `FileStream`, and ordinary `System.IO` APIs.

It already provides:

- `GET/HEAD /pkg`
- `GET/HEAD /pkg/{id}`
- `GET/HEAD /catalog`
- `GET/HEAD /icon/{id}`
- PS4 manifest support under `/json/{id}.json`
- in-memory icon registration
- multiple registered file IDs
- optional seekable `IRangeSource`
- per-file and aggregate served-byte accounting
- revoke/unrevoke semantics used by queue cancellation/resume

The byte-range implementation is already substantial:

- parses `Range: bytes=start-end`
- supports open-ended ranges
- supports suffix ranges
- returns HTTP 206 with `Content-Range`
- returns HTTP 416 when the start/end is unsatisfiable
- emits `Accept-Ranges: bytes`
- supports HEAD without sending the body
- streams from a seeked `FileStream`
- uses a bounded 256 KiB transfer buffer rather than reading a whole PKG into memory

Therefore the NAS implementation must **reuse or refactor this implementation**, not introduce a second independent Range implementation.

### Existing PS5 install client is reusable

`LoopDPI.Core/ConsoleClient.cs` already performs the receiver installation control path:

- probes the receiver
- posts to `/api/install`
- supports name and `icon_url`
- checks `/api/status`
- supports receiver pull/pause/cancel helpers

For the Synology PKG install MVP, the intended data path remains:

`NAS RangeFileServer :9898 -> PS5 pulls PKG`

and the control path remains:

`NAS ConsoleClient -> PS5 :12800 /api/install`

No new PS5 installation protocol is required.

### Discovery protocol is already in Core

`LoopDPI.Core/NetDiscovery.cs` defines:

- receiver port: TCP 12800
- receiver beacon: UDP 12801, magic `PKGSENDER`
- sender/catalog announce: UDP 12802, magic `PKGSENDER-PC`
- sender announce interval: 3 seconds
- listener for receiver beacons
- subnet-aware IPv4 NIC enumeration and optional TCP sweep

This should be reused under Synology. Docker/Container Manager host networking remains the preferred deployment mode because broadcast discovery must reach the LAN.

## Cross-platform assessment of LoopDPI.Core

### Positive finding

Most of `LoopDPI.Core` is ordinary .NET 8 code and has no direct Win32/PInvoke/Registry/System.Drawing dependency. In particular, the network and streaming pieces required by the NAS MVP are platform-neutral.

### Blocking/cleanup finding: Avalonia leaks into Core

`LoopDPI.Core/LoopDPI.Core.csproj` references Avalonia 11.3.8 because `PkgInfo.cs` imports `Avalonia.Media.Imaging` and exposes:

`PkgInfo.Cover : Bitmap?`

The parser/data model already stores the correct transport-neutral representation as `IconData : byte[]?`. The `Cover` property is presentation logic and should not be part of the Core model.

Required cleanup before treating Core as a clean headless dependency:

1. keep `IconData` in `PkgInfo`;
2. move bitmap decoding/projection to the Avalonia desktop project;
3. remove Avalonia from `LoopDPI.Core.csproj` if no remaining Core file requires it.

This is a structural cleanup, not a parser rewrite.

### Desktop persistence assumptions

`AppSettings.cs` stores desktop configuration under `Environment.SpecialFolder.ApplicationData/LoopDPI-Sender`. This API itself is cross-platform, but those defaults and fields are desktop-specific and should not define the NAS contract.

`ConsoleClient.PushAsync` also writes `push-debug.log` under desktop application data. The protocol client is reusable, but logging should be injected or routed through host logging so a headless container does not silently write to a desktop-oriented path.

`GameLibrary.PythonHeader` is mostly portable, but bridge discovery currently checks:

- `AppContext.BaseDirectory/pkg_header.py`
- desktop ApplicationData
- a hard-coded development path `D:\OpenCode\pkg-sender\library\pkg_header.py`

A Linux container can use the first path if the bridge is packaged next to the app. The Windows development path must not be relied on.

## Detailed porting classification

Classification is intentionally applied to the smallest useful code unit when a file mixes domain and UI concerns.

| Code / responsibility | Classification | Reason / required action |
| --- | --- | --- |
| `RangeFileServer`, `IRangeSource`, Range parser/streaming | **REUSE** | Core NAS data plane; raw sockets and file APIs are cross-platform. Preserve behavior and add regression tests before changes. |
| `CatalogEntry` transport model | **REUSE** | Already Core and matches the PS5 console catalog contract. |
| `NetDiscovery` beacon listen / sender announce / NIC logic | **REUSE** | Cross-platform .NET networking. Host networking is required for reliable container broadcast. |
| `ConsoleClient` receiver probe/install/status protocol | **REUSE** | Correct protocol seam for NAS -> PS5 control. Replace hard-coded desktop debug-log side effect with host logging later. |
| `ReceiverClient` | **REUSE** | Cross-platform receiver file API. Needed for later image/file-copy features, not MVP install. |
| `TransferManager` | **REUSE** | Cross-platform streaming/resume queue for file-copy operations; not needed for PKG pull install MVP. |
| `PkgReader` and PKG parsing in `PkgInfo.cs` | **REUSE** | Stream-based parser is headless-friendly. |
| `PkgInfo` scalar metadata + `IconData` | **REUSE** | Suitable domain model fields. |
| `PkgInfo.Cover : Avalonia Bitmap` | **WINDOWS_ONLY** | Presentation projection belongs in Avalonia desktop layer; remove from Core model and recreate as desktop helper/property. |
| Avalonia package reference in `LoopDPI.Core.csproj` | **MOVE_TO_CORE** | This row represents boundary correction: after moving the Cover projection out of Core, Core should no longer depend on Avalonia. The actual UI dependency remains in desktop. |
| `GameReader`, folder/image readers in `GameLibrary.cs` | **REUSE** | Mostly ordinary file/stream parsing. |
| `PythonHeader` parser bridge | **REWRITE_FOR_SERVER** | Parsing contract can stay; interpreter/bridge discovery must be deterministic in a container and not depend on desktop/developer paths. |
| `library/LibraryScanner.cs` scanning/caching | **MOVE_TO_CORE** | Scanner is domain/headless logic currently located in the desktop project. Cache root must become injectable rather than fixed ApplicationData. |
| role derivation in `LibraryView.ToGameItem` (Image/DLC/Patch/Game) | **MOVE_TO_CORE** | Domain rule currently embedded in UI. NAS catalog must use exactly the same rule. |
| `FamilyKeyOf` exact-TitleId family rule | **MOVE_TO_CORE** | Domain rule; exact TitleId prevents cross-region family merging and must be shared by desktop/NAS. |
| catalog construction in `LibraryView.BuildCatalog` | **MOVE_TO_CORE** | Domain/catalog mapping currently tied to UI state and server registration. Extract a catalog builder/service consumed by both hosts. |
| base-cover fallback / effective icon selection | **MOVE_TO_CORE** | Catalog semantics, not visual widget behavior. Needed so patch/DLC cards receive base-game covers on console. |
| server lifecycle currently in `LibraryView.EnsureServer` | **REWRITE_FOR_SERVER** | NAS needs host lifetime ownership/DI/cancellation/logging rather than a View owning the listener. The underlying RangeFileServer is still reused. |
| desktop queue orchestration in `LibraryView` | **WINDOWS_ONLY** | UI state, dispatcher posting, button behavior, Avalonia bindings remain desktop. Domain queue concepts can use existing Core types. |
| `AppSettings.cs` as NAS configuration | **REWRITE_FOR_SERVER** | Create NAS options/environment config. Do not change desktop settings contract solely for NAS. |
| `UpdateService.cs` / desktop installer launching | **WINDOWS_ONLY** | Not appropriate inside a Synology container. |
| `library/*.axaml*`, desktop windows/views | **WINDOWS_ONLY** | Avalonia desktop host is preserved, not ported. |
| `droid/*` Android UI/SAF host | **OUT_OF_SCOPE** | Independent Android target; do not couple NAS implementation to Android-specific lifecycle/SAF. |
| `payload/main.c`, PS5 receiver build | **OUT_OF_SCOPE** | Receiver is the protocol peer/reference and should remain unchanged for NAS MVP. |
| `preview-ui.html`, mockups | **OUT_OF_SCOPE** | Reference/testing assets, not NAS runtime source of truth. |
| PS4 GoldHEN-specific installer path | **OUT_OF_SCOPE** | Preserve existing Core behavior, but Synology PS5 MVP should not expand PS4 scope. |

Note: the classification label `MOVE_TO_CORE` for the Avalonia boundary means “perform the extraction necessary so Core becomes headless”; the Avalonia type itself should move *out of* Core into the desktop project.

## Domain logic currently trapped in the desktop View

The largest design issue is not the protocol server. It is that `library/Views/LibraryView.axaml.cs` currently owns several rules required by a headless catalog:

### Role calculation

Current rule:

- non-`pkg` -> `Image`
- `PkgInfo.IsDlc` -> `DLC`
- `ContentType == "gp"` -> `Patch`
- otherwise -> `Game`

This must become a shared domain helper.

### Family key calculation

Current rule uses exact normalized `TitleId`, otherwise a per-file key:

- valid TitleId -> uppercase exact TitleId
- no usable TitleId -> `FILE:<basename>`

This exact-region behavior is documented in code and should not be reimplemented independently in NAS.

### Catalog construction

`BuildCatalog()` currently:

- snapshots UI library rows;
- excludes folders;
- registers ID -> local path in the Range server registry;
- unrevoke IDs on fresh catalog intent;
- applies effective/base icon fallback;
- registers icon bytes;
- emits `CatalogEntry` including role/family/platform/format/file/hasIcon.

This should become a headless-compatible catalog service consumed by both desktop and NAS hosts.

## Port and host architecture finding

The earlier conceptual NAS plan assumed an ASP.NET Core host and the existing package server could both live on port 9898. The source shows that `RangeFileServer` itself binds `IPAddress.Any:9898` and owns the HTTP protocol.

Therefore a new Kestrel host cannot also bind 9898 in the same container/process.

### Safest M1 transition

M1 should keep the current data-plane server untouched and introduce a small ASP.NET Core management host on a separate configurable port, for example:

- package/catalog data plane: existing `RangeFileServer` on **9898**
- management/Web API health plane: ASP.NET Core on **9899** initially

Suggested M1 options:

- `LOOPDPI_PACKAGE_PORT=9898`
- `LOOPDPI_WEB_PORT=9899`
- `LOOPDPI_PACKAGE_PATH=/packages`
- `LOOPDPI_DATA_PATH=/data`
- `LOOPDPI_PS5_IP=`
- `LOOPDPI_PS5_PORT=12800`
- `LOOPDPI_PUBLIC_HOST=`

This yields the smallest safe headless process without duplicating or destabilizing the working Range implementation.

### If a single public port is mandatory

Do not copy the Range code into an ASP.NET controller.

Instead create a later refactor gate:

1. extract the Range request/range-resolution/catalog semantics from `RangeFileServer` into reusable Core services;
2. keep the raw TCP adapter for desktop compatibility;
3. add a Kestrel adapter using the same Core semantics;
4. only then converge Web UI and PKG data onto one port.

That is a larger M2/M2.5 change and should be protected by protocol regression tests.

## Recommended M1 extraction/refactor sequence

M1 should remain small and reversible.

1. **Decouple `PkgInfo` from Avalonia**
   - remove `Cover` from the Core data model;
   - add equivalent desktop-only cover decoding/projection;
   - remove Avalonia reference from Core when no longer used.
2. **Extract library domain rules**
   - move role calculation to Core;
   - move family key calculation to Core;
   - move catalog mapping/effective-icon logic to a reusable Core service.
3. **Move/refactor `LibraryScanner`**
   - create a Core/headless scanner abstraction;
   - inject cache/data root;
   - preserve current recursive walk, reparse-point avoidance and cache-version behavior.
4. **Introduce NAS configuration types**
   - do not reuse desktop `AppSettings` as the NAS persistence contract;
   - bind environment/options to `/packages` and `/data`.
5. **Create `LoopDPI.Nas` ASP.NET Core project**
   - .NET 8;
   - `GET /health`;
   - separate initial Web/management port;
   - no package-serving rewrite yet.
6. **Add lifecycle wrappers**
   - host existing `RangeFileServer` as a managed background service only when M2 enables PKG serving;
   - route logs through `ILogger`.
7. **Preserve desktop behavior**
   - desktop continues using the same Core parser/server/protocol components;
   - no protocol fork.

## M1 acceptance gate

M1 is **GO** if all of the following are true:

- Core no longer requires Avalonia solely for `PkgInfo.Cover`;
- shared role/family/catalog rules exist outside Avalonia views;
- scanner/cache root can be supplied by a headless host;
- `LoopDPI.Nas` can build for Linux .NET 8;
- existing desktop code still builds on a machine with the required SDK/dependencies;
- no duplicate `/pkg` or Range implementation has been introduced.

M1 is **NO-GO** for a single-port Kestrel rewrite until Range protocol regression tests exist.

## Validation performed in M0

### Git baseline

Verified after clone:

- branch: `main`
- HEAD: `f879995bd9e3a8bf67bf22cb94152c224db5d732`
- initial worktree: clean

### Static source inspection

Verified directly in source:

- existing 9898 HTTP/Range server and 206/416/HEAD behavior;
- existing catalog/icon endpoints;
- receiver `/api/install` control client;
- UDP 12801 receiver discovery;
- UDP 12802 sender announcement;
- desktop-owned scanner, role/family and catalog logic;
- Core Avalonia dependency source;
- desktop ApplicationData/debug-log assumptions;
- existing transfer/resume manager.

### .NET build/test limitation

Baseline `dotnet` build/test commands could not be started because the WebCodex Runner host does not currently expose a `dotnet` executable in PATH.

Observed checks:

- structured process invocation of `dotnet --info`: rejected before spawn because the executable was not found;
- `/usr/bin/which dotnet`: exit status 1 with no path.

This is an **environment/toolchain limitation, not a source build failure**. M0 did not install a .NET SDK because the audit task should not mutate the host toolchain merely to obtain a baseline.

Once a .NET 8 SDK is available, run at minimum:

```sh
dotnet build LoopDPI.Core/LoopDPI.Core.csproj --nologo
dotnet build library/PkgSender.csproj --nologo
dotnet build tests/sendtest/sendtest.csproj --nologo
dotnet build tests/srvtest/srvtest.csproj --nologo
```

Android validation should be attempted only on a host with the required .NET Android workload; failure due to missing Android workload is not a NAS/Core regression.

## Risks to carry into M1/M2

1. **Protocol regression risk:** `RangeFileServer` is already production behavior. Refactor only behind tests for GET, HEAD, open-ended/suffix ranges, 206, 416, disconnect/reconnect, and large-file streaming.
2. **UI/domain divergence risk:** role/family/catalog logic currently lives in Avalonia View code. NAS must share it, not copy it.
3. **Port collision risk:** raw Range server currently owns 9898; Kestrel cannot use the same address simultaneously.
4. **Container discovery risk:** UDP broadcast may not work as intended behind bridge networking; Synology host networking is preferred.
5. **Package-parser dependency risk:** Python fallback/image parsing must use deterministic container packaging and must not depend on the hard-coded Windows development path.
6. **Persistence risk:** desktop ApplicationData paths must not leak into container configuration/cache/logging.
7. **Validation gap:** current Runner lacks .NET SDK, so compile/test evidence must be obtained before merging structural M1 changes.

## Final M0 recommendation

Proceed to M1, but treat it as a **boundary extraction + headless bootstrap** stage rather than a protocol rewrite.

The shortest safe path is:

`existing Core protocol/data plane`
-> `remove Avalonia leakage + extract scanner/catalog rules`
-> `new ASP.NET Core NAS management host`
-> `reuse existing RangeFileServer on 9898`
-> `real PS5 install MVP`

Do not build a second PKG server, do not copy family/role rules into the NAS project, and do not merge Kestrel onto port 9898 until the shared Range semantics have regression coverage.
