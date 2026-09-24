# Release Notes

[中文发布说明](RELEASE_NOTES.zh-CN.md)

## 0.1.0-0021 - Synology NAS/SPK MVP

This release packages the Synology NAS implementation as a DSM 7 SPK and validates it on a DS1517+ x86_64 target.

### Highlights

- Native Synology DSM package (`PKGSenderNAS`) with DSM UI entry.
- Web UI for browsing PS5 PKG files from the NAS.
- Configurable PS5 receiver IP from the Web UI.
- Configurable PKG library paths from the Web UI, including multiple NAS paths.
- Recursive multi-library PKG scan with opaque package IDs.
- PS5 receiver install submission via `POST http://<ps5>:12800/api/install`.
- HTTP `GET|HEAD /pkg/{id}` serving with Range support through Go `http.ServeContent`.
- Package metadata parsing for title, localized titles, Title ID, Content ID, version, and package type.
- Family grouping by Title ID with Game / Patch / DLC / App ordering.
- Local title alias import/export flow for manual Chinese title curation.
- Persistent install history and FIFO queue with retry, cancel, and reorder support.
- UDP receiver discovery status display.
- Chinese / English Web UI language switch.
- GitHub Container Registry Docker image: `ghcr.io/mrggbond/pkg-sender-synology` for `linux/amd64` and `linux/arm64`.

### Not included

- NAS-side PS5 payload sending was removed. `pkg-receiver.elf` must be loaded on the PS5 through the user's exploit/WebKit flow.
- PS4 / GoldHEN workflow is not part of the Synology SPK MVP.
- Authentication, TLS, and WAN exposure are intentionally out of scope. Use on a trusted LAN only.

### Validated target

- Synology DSM 7.2.2
- DS1517+ / x86_64
- SPK: `PKGSenderNAS-0.1.0-0021-x86_64.spk`
- ARMv8 SPK was cross-built and structurally verified: `PKGSenderNAS-0.1.0-0021-armv8.spk`

### Release assets

- `PKGSenderNAS-0.1.0-0021-x86_64.spk`
- `PKGSenderNAS-0.1.0-0021-armv8.spk`
- `pkg-receiver.elf`
- Docker image: `ghcr.io/mrggbond/pkg-sender-synology:0.1.0-0021`

### SHA256

```text
277626ae1a6715c729a187e3d5102032eb4a1cc93c892f00710863bda66c7c33  PKGSenderNAS-0.1.0-0021-x86_64.spk
d946c43e6b12c6945f41e3008159acee5d232f10b06e7d8b67b940ff5488569a  PKGSenderNAS-0.1.0-0021-armv8.spk
f281fde2d62353cfd0b263b48815bdae64bc2527d5ded2e2ff9956d9c87b1cb2  pkg-receiver.elf
```

### Validation summary

- `go test ./...`
- `go vet ./...`
- Web UI JavaScript parse gate
- `git diff --check`
- SPK x86_64 build and structure verification
- SPK armv8 build and structure verification
- Docker image workflow added for GHCR multi-architecture publishing
- In-place NAS upgrade to `0.1.0-0021`
- Health/API/UI checks after deployment
