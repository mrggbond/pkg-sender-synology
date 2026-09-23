# Release Notes

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

### Not included

- NAS-side PS5 payload sending was removed. `pkg-receiver.elf` must be loaded on the PS5 through the user's exploit/WebKit flow.
- PS4 / GoldHEN workflow is not part of the Synology SPK MVP.
- Authentication, TLS, and WAN exposure are intentionally out of scope. Use on a trusted LAN only.

### Validated target

- Synology DSM 7.2.2
- DS1517+ / x86_64
- SPK: `PKGSenderNAS-0.1.0-0021-x86_64.spk`

### Validation summary

- `go test ./...`
- `go vet ./...`
- Web UI JavaScript parse gate
- `git diff --check`
- SPK x86_64 build and structure verification
- In-place NAS upgrade to `0.1.0-0021`
- Health/API/UI checks after deployment
