#!/bin/sh
set -eu

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
NAS_DIR="$(CDPATH= cd -- "${SCRIPT_DIR}/.." && pwd)"
OUT_DIR="${OUT_DIR:-${SCRIPT_DIR}/dist}"
VERSION="${VERSION:-0.1.0-0022}"
ARCH="${ARCH:-x86_64}"
GO_BIN="${GO_BIN:-go}"

base64_one_line() {
    base64 <"$1" | tr -d '\r\n'
}

md5_file() {
    if command -v md5sum >/dev/null 2>&1; then
        md5sum "$1" | awk '{print $1}'
    elif command -v md5 >/dev/null 2>&1; then
        md5 -q "$1"
    else
        openssl md5 -r "$1" | awk '{print $1}'
    fi
}

case "${ARCH}" in
    x86_64|avoton)
        GOARCH="amd64"
        INFO_ARCH="x86_64"
        ;;
    armv8|aarch64|arm64)
        GOARCH="arm64"
        INFO_ARCH="armv8"
        ;;
    *)
        echo "Unsupported ARCH: ${ARCH}. Use x86_64 or armv8." >&2
        exit 2
        ;;
esac

command -v "${GO_BIN}" >/dev/null 2>&1 || {
    echo "Go tool not found: ${GO_BIN}" >&2
    exit 2
}

case "${VERSION}" in
    *[!0-9A-Za-z._-]*|"")
        echo "Invalid VERSION: ${VERSION}" >&2
        exit 2
        ;;
esac

TMP_ROOT="$(mktemp -d "${TMPDIR:-/tmp}/pkgsender-dsm6-spk.XXXXXX")"
trap 'rm -rf "${TMP_ROOT}"' EXIT HUP INT TERM

SPK_ROOT="${TMP_ROOT}/spk"
PAYLOAD="${TMP_ROOT}/payload"
mkdir -p "${SPK_ROOT}/scripts"
mkdir -p "${PAYLOAD}/bin" "${PAYLOAD}/share" "${PAYLOAD}/var" "${PAYLOAD}/ui/images" "${OUT_DIR}"

echo "Running native Go tests..."
(
    cd "${NAS_DIR}"
    "${GO_BIN}" test ./...
)

echo "Building DSM6 linux/${GOARCH} static binary..."
(
    cd "${NAS_DIR}"
    CGO_ENABLED=0 GOOS=linux GOARCH="${GOARCH}" GOAMD64="${GOAMD64:-v1}" "${GO_BIN}" build \
        -trimpath -ldflags="-s -w" \
        -o "${PAYLOAD}/bin/pkg-sender-nas" ./cmd/pkg-sender-nas
)

cp "${SCRIPT_DIR}/dsm6/config.env.example" "${PAYLOAD}/share/config.env.example"
cp "${SCRIPT_DIR}/payload/var/PKGSenderNAS.sc" "${PAYLOAD}/var/PKGSenderNAS.sc"
cp "${SCRIPT_DIR}/payload/ui/config" "${PAYLOAD}/ui/config"
cp "${SCRIPT_DIR}/payload/ui/index.html" "${PAYLOAD}/ui/index.html"
cp "${SCRIPT_DIR}/PACKAGE_ICON.PNG" "${PAYLOAD}/ui/images/icon_16.png"
cp "${SCRIPT_DIR}/PACKAGE_ICON.PNG" "${PAYLOAD}/ui/images/icon_32.png"

sed \
    -e "s/@VERSION@/${VERSION}/g" \
    -e "s/@ARCH@/${INFO_ARCH}/g" \
    "${SCRIPT_DIR}/INFO.dsm6.in" >"${SPK_ROOT}/INFO"

cp "${SCRIPT_DIR}/PACKAGE_ICON.PNG" "${SPK_ROOT}/PACKAGE_ICON.PNG"
cp "${SCRIPT_DIR}/PACKAGE_ICON_256.PNG" "${SPK_ROOT}/PACKAGE_ICON_256.PNG"

printf 'package_icon="%s"\n' "$(base64_one_line "${SCRIPT_DIR}/PACKAGE_ICON.PNG")" >>"${SPK_ROOT}/INFO"
printf 'package_icon_256="%s"\n' "$(base64_one_line "${SCRIPT_DIR}/PACKAGE_ICON_256.PNG")" >>"${SPK_ROOT}/INFO"
for script in start-stop-status postinst preinst preuninst preupgrade postupgrade postuninst; do
    if [ "${script}" = "start-stop-status" ]; then
        cp "${SCRIPT_DIR}/dsm6/start-stop-status" "${SPK_ROOT}/scripts/${script}"
    else
        cp "${SCRIPT_DIR}/scripts/${script}" "${SPK_ROOT}/scripts/${script}"
    fi
done

chmod 755 "${PAYLOAD}/bin/pkg-sender-nas"
chmod 644 "${PAYLOAD}/share/config.env.example" "${PAYLOAD}/var/PKGSenderNAS.sc" "${PAYLOAD}/ui/config" "${PAYLOAD}/ui/index.html" "${PAYLOAD}/ui/images/"*.png
chmod 755 "${SPK_ROOT}/scripts/"*
chmod 644 "${SPK_ROOT}/INFO" "${SPK_ROOT}/PACKAGE_ICON.PNG" "${SPK_ROOT}/PACKAGE_ICON_256.PNG"

export COPYFILE_DISABLE=1
tar --no-xattrs -cJf "${SPK_ROOT}/package.tgz" -C "${PAYLOAD}" .

EXTRACT_SIZE="$(du -sk "${PAYLOAD}" | awk '{print $1}')"
CHECKSUM="$(md5_file "${SPK_ROOT}/package.tgz")"
printf 'extractsize="%s"\n' "${EXTRACT_SIZE}" >>"${SPK_ROOT}/INFO"
printf 'checksum="%s"\n' "${CHECKSUM}" >>"${SPK_ROOT}/INFO"
printf 'create_time="%s"\n' "$(date '+%Y%m%d-%H:%M:%S')" >>"${SPK_ROOT}/INFO"

OUT="${OUT_DIR}/PKGSenderNAS-${VERSION}-dsm6-${INFO_ARCH}.spk"
rm -f "${OUT}"
tar --no-xattrs -cf "${OUT}" -C "${SPK_ROOT}" INFO package.tgz scripts PACKAGE_ICON.PNG PACKAGE_ICON_256.PNG

"${SCRIPT_DIR}/verify-dsm6.sh" "${OUT}"
echo "${OUT}"
