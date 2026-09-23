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

- version: `0.1.0-0002`
- architecture: `x86_64`
- output: `dist/PKGSenderNAS-0.1.0-0002-x86_64.spk`

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
PKGSENDER_LISTEN=":9898"
PKGSENDER_PUBLIC_BASE_URL="http://192.168.32.5:9898"
PKGSENDER_PS5_IP="192.168.32.100"
PKGSENDER_PS5_PORT="12800"
```

If the config is missing or still contains `CHANGE_ME`, the lifecycle script reports that configuration is required and deliberately does not launch the daemon. This prevents an initial package install from failing solely because the NAS/PS5 addresses have not been entered yet.

## Shared-folder permission

The native process runs as the package identity rather than root. In DSM Shared Folder permissions, grant the system-internal user associated with **PS5 PKG Sender / PKGSenderNAS** read/traverse access to the directory configured by `PKGSENDER_PACKAGE_DIR`. Write permission is not required.

On DSM installations where the shared folder uses Synology ACLs, Unix mode bits alone may not be sufficient. Grant the package identity read/traverse access to the library path and its existing descendants. The DS1517+ migration target required an explicit `PKGSenderNAS` read/traverse ACE because the shared-folder ACL otherwise denied the package user even though the directory mode was permissive.

## Runtime files

Persistent package data:

```text
/var/packages/PKGSenderNAS/var/
├── config.env
├── pkg-sender-nas.log
└── pkg-sender-nas.pid
```

Immutable installed payload:

```text
/var/packages/PKGSenderNAS/target/
├── bin/pkg-sender-nas
└── share/config.env.example
```

The package intentionally does not declare `adminport` or a DSM `port-config` for 9898. On the migration target, WebStation already has the existing Docker-project service registered against `127.0.0.1:9898`; declaring the same port again makes DSM reject the SPK with error 283. The native daemon still binds the configured `PKGSENDER_LISTEN` address (default `:9898`) once the Docker service is stopped.

## Safe DSM validation

`verify.sh` is the primary pre-install gate. It checks the SPK layout, lifecycle script syntax, required DSM metadata, embedded icon fields, payload contents, executable mode, absence of duplicate DSM port ownership, and the MD5 recorded for `package.tgz`.

For hardware validation without installation, copy the SPK to the NAS, unpack it into a temporary directory, and execute the extracted binary with an empty environment. On the DS1517+ test target the x86_64 binary executes and fails closed on the missing `PKGSENDER_PS5_IP`, which proves the packaged Linux binary is runnable without binding a port.

DSM 7.2.2 also exposes `synopkg query <spk>`, but it is not used as a blocking validation gate here. On the test NAS it returns the same generic failure for a temporary SPK reconstructed from an already-installed custom package, so that result is not specific enough to distinguish a malformed hand-built SPK.


## Real-hardware migration acceptance

DSM 7.2.2 on DS1517+ (`x86_64`, `avoton`) has passed the native-package migration gate with `0.1.0-0002`:

- SPK installation completed successfully after removing duplicate DSM ownership of port 9898;
- the daemon runs as `PKGSenderNAS:PKGSenderNAS`, not root;
- the package identity can read the existing PS5 PKG library through Synology ACLs;
- `synopkg restart PKGSenderNAS` performs a clean stop/start and rescans the library;
- `/health` reports all 5 PKGs;
- family metadata and local covers are available;
- HTTP Range remains `206 Partial Content`;
- the former Docker container remains stopped as a rollback path.

No real PS5 install was triggered during this migration gate.

Do **not** install or start the native SPK while the stable Docker service is still bound to port 9898. Installation/start testing is a separate migration gate after explicitly stopping the Docker instance.
