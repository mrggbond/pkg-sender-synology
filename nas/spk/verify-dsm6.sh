#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 path/to/PKGSenderNAS-*-dsm6-*.spk" >&2
    exit 2
fi

SPK="$1"
[ -f "${SPK}" ] || {
    echo "SPK not found: ${SPK}" >&2
    exit 2
}

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/pkgsender-dsm6-spk-verify.XXXXXX")"
trap 'rm -rf "${TMP_ROOT}"' EXIT HUP INT TERM

TOP="${TMP_ROOT}/top"
PAYLOAD="${TMP_ROOT}/payload"
mkdir -p "${TOP}" "${PAYLOAD}"

tar -xf "${SPK}" -C "${TOP}"

for required in \
    INFO \
    package.tgz \
    PACKAGE_ICON.PNG \
    PACKAGE_ICON_256.PNG \
    scripts/start-stop-status \
    scripts/postinst \
    scripts/preinst \
    scripts/preuninst \
    scripts/preupgrade \
    scripts/postupgrade \
    scripts/postuninst
do
    [ -f "${TOP}/${required}" ] || {
        echo "Missing SPK entry: ${required}" >&2
        exit 1
    }
done

grep -q '^package="PKGSenderNAS"$' "${TOP}/INFO"
grep -q '^os_min_ver="6\.0-7321"$' "${TOP}/INFO"
if grep -q '^os_min_ver="7\.' "${TOP}/INFO"; then
    echo "DSM6 SPK must not declare a DSM7 minimum version." >&2
    exit 1
fi
grep -q '^startable="yes"$' "${TOP}/INFO"
grep -q '^ctlscript="start-stop-status"$' "${TOP}/INFO"
grep -q '^maintainer="mrggbond"$' "${TOP}/INFO"
grep -q '^dsmuidir="ui"$' "${TOP}/INFO"
grep -q '^dsmappname="PKGSenderNAS"$' "${TOP}/INFO"
if grep -q '^adminport=' "${TOP}/INFO"; then
    echo "DSM6 SPK must not declare adminport; TCP 9898 is daemon-configured." >&2
    exit 1
fi
grep -q '^package_icon="' "${TOP}/INFO"
grep -q '^package_icon_256="' "${TOP}/INFO"
grep -Eq '^extractsize="[0-9]+"$' "${TOP}/INFO"
grep -Eq '^checksum="[0-9a-fA-F]{32}"$' "${TOP}/INFO"
grep -Eq '^create_time="[0-9]{8}-[0-9]{2}:[0-9]{2}:[0-9]{2}"$' "${TOP}/INFO"

for script in "${TOP}/scripts/"*; do
    /bin/sh -n "${script}"
done
grep -q 'PKGSENDER_HISTORY_FILE' "${TOP}/scripts/start-stop-status"
grep -q 'PKGSENDER_CONFIG_FILE' "${TOP}/scripts/start-stop-status"
grep -q 'VAR_DIR}/packages' "${TOP}/scripts/start-stop-status"
if grep -q 'installed but not configured' "${TOP}/scripts/start-stop-status"; then
    echo "DSM6 friendly package must not refuse to start before Web UI configuration." >&2
    exit 1
fi

tar -xf "${TOP}/package.tgz" -C "${PAYLOAD}"

if command -v md5sum >/dev/null 2>&1; then
    ACTUAL_MD5="$(md5sum "${TOP}/package.tgz" | awk '{print $1}')"
elif command -v md5 >/dev/null 2>&1; then
    ACTUAL_MD5="$(md5 -q "${TOP}/package.tgz")"
else
    ACTUAL_MD5="$(openssl md5 -r "${TOP}/package.tgz" | awk '{print $1}')"
fi
EXPECTED_MD5="$(awk -F'"' '/^checksum="/ {print $2; exit}' "${TOP}/INFO")"
[ "${ACTUAL_MD5}" = "${EXPECTED_MD5}" ] || {
    echo "package.tgz checksum mismatch." >&2
    exit 1
}

for required in \
    bin/pkg-sender-nas \
    share/config.env.example \
    var/PKGSenderNAS.sc \
    ui/config \
    ui/index.html \
    ui/images/icon_16.png \
    ui/images/icon_32.png
do
    [ -f "${PAYLOAD}/${required}" ] || {
        echo "Missing payload entry: ${required}" >&2
        exit 1
    }
done

[ -x "${PAYLOAD}/bin/pkg-sender-nas" ] || {
    echo "Payload binary is not executable." >&2
    exit 1
}

grep -q 'PKGSENDER_PACKAGE_DIR="/var/packages/PKGSenderNAS/var/packages"' "${PAYLOAD}/share/config.env.example"
grep -q 'PKGSENDER_PUBLIC_BASE_URL=""' "${PAYLOAD}/share/config.env.example"
grep -q 'PKGSENDER_PS5_IP=""' "${PAYLOAD}/share/config.env.example"
if grep -q 'CHANGE_ME_' "${PAYLOAD}/share/config.env.example"; then
    echo "DSM6 friendly config must not require CHANGE_ME placeholders." >&2
    exit 1
fi

grep -q 'dst\.ports="12801/udp"' "${PAYLOAD}/var/PKGSenderNAS.sc"
if grep -q '9898' "${PAYLOAD}/var/PKGSenderNAS.sc"; then
    echo "SPK must not re-register TCP 9898 through DSM port-config." >&2
    exit 1
fi

echo "DSM6 SPK structure verification: PASS"
