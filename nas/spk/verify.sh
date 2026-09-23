#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 path/to/PKGSenderNAS-*.spk" >&2
    exit 2
fi

SPK="$1"
[ -f "${SPK}" ] || {
    echo "SPK not found: ${SPK}" >&2
    exit 2
}

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/pkgsender-spk-verify.XXXXXX")"
trap 'rm -rf "${TMP_ROOT}"' EXIT HUP INT TERM

TOP="${TMP_ROOT}/top"
PAYLOAD="${TMP_ROOT}/payload"
mkdir -p "${TOP}" "${PAYLOAD}"

tar -xf "${SPK}" -C "${TOP}"

for required in     INFO     package.tgz     PACKAGE_ICON.PNG     PACKAGE_ICON_256.PNG     conf/privilege     conf/resource     scripts/start-stop-status     scripts/postinst     scripts/preinst     scripts/preuninst     scripts/preupgrade     scripts/postupgrade     scripts/postuninst
do
    [ -f "${TOP}/${required}" ] || {
        echo "Missing SPK entry: ${required}" >&2
        exit 1
    }
done

grep -q '^package="PKGSenderNAS"$' "${TOP}/INFO"
grep -q '^os_min_ver="7\.0-40000"$' "${TOP}/INFO"
grep -q '^startable="yes"$' "${TOP}/INFO"
grep -q '^ctlscript="start-stop-status"$' "${TOP}/INFO"
grep -q '^maintainer="mrggbond"$' "${TOP}/INFO"
grep -q '^dsmuidir="ui"$' "${TOP}/INFO"
grep -q '^dsmappname="PKGSenderNAS"$' "${TOP}/INFO"
if grep -q '^adminport=' "${TOP}/INFO"; then
    echo "SPK must not declare adminport; DSM reports a port conflict on 9898." >&2
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
for required in     bin/pkg-sender-nas     share/config.env.example     var/PKGSenderNAS.sc     ui/config     ui/index.html     ui/images/icon_16.png     ui/images/icon_32.png
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

grep -q '"port-config"' "${TOP}/conf/resource"
grep -q 'dst\.ports="12801/udp"' "${PAYLOAD}/var/PKGSenderNAS.sc"
grep -q 'port_forward="no"' "${PAYLOAD}/var/PKGSenderNAS.sc"
if grep -q '9898' "${PAYLOAD}/var/PKGSenderNAS.sc"; then
    echo "SPK must not re-register port 9898 through DSM port-config." >&2
    exit 1
fi
grep -q 'CHANGE_ME_NAS_IP' "${PAYLOAD}/share/config.env.example"
grep -q 'CHANGE_ME_PS5_IP' "${PAYLOAD}/share/config.env.example"

echo "SPK structure verification: PASS"
