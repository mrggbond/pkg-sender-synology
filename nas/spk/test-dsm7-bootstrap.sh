#!/bin/sh
# DSM7 first-run and upgrade regression test (no Synology root required).
set -eu
HERE="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/pkgsender-dsm7-bootstrap.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT HUP INT TERM
PACKAGE_ROOT="$TMP/PKGSenderNAS"
VAR_DIR="$PACKAGE_ROOT/var"
mkdir -p "$VAR_DIR" "$TMP/bin"
sed -e "s|^PACKAGE_ROOT=.*|PACKAGE_ROOT=\"$PACKAGE_ROOT\"|" \
    -e "s|/var/packages/PKGSenderNAS/var/packages|$VAR_DIR/packages|g" \
    -e "s|/volume1/PS5/PKG|$TMP/missing-old-default|g" \
    "$HERE/scripts/start-stop-status" | sed '/^case "${1:-}" in/,$d' > "$TMP/start-functions.sh"
sed "s|/var/packages/PKGSenderNAS/var/packages|$VAR_DIR/packages|g" \
    "$HERE/payload/share/config.env.example" > "$VAR_DIR/config.env"
cp "$VAR_DIR/config.env" "$TMP/fresh-config.snapshot"

sh -eu -c '
  . "$1"
  load_config
  [ "$PKGSENDER_PS5_IP" = "" ]
  [ "$PKGSENDER_PUBLIC_BASE_URL" = "" ]
  [ "$PKGSENDER_PACKAGE_DIR" = "$VAR_DIR/packages" ]
  env | grep -q "^PKGSENDER_PACKAGE_DIR="
' sh "$TMP/start-functions.sh"
cmp "$TMP/fresh-config.snapshot" "$VAR_DIR/config.env"
printf 'fresh config: PASS\n'

cat > "$TMP/bin/fake-daemon" <<'EOS'
#!/bin/sh
sleep 10
EOS
chmod +x "$TMP/bin/fake-daemon"
sh -eu -c '
  . "$1"
  BIN="$2"
  pid_is_running() { [ -s "$PID_FILE" ]; }
  start_daemon
  [ -d "$VAR_DIR/packages" ]
  kill "$(cat "$PID_FILE")" 2>/dev/null || :
' sh "$TMP/start-functions.sh" "$TMP/bin/fake-daemon"
printf 'first-run start and default directory creation: PASS\n'

cat > "$VAR_DIR/config.env" <<EOS
PKGSENDER_PACKAGE_DIR="$TMP/missing-old-default"
PKGSENDER_LISTEN=":9898"
PKGSENDER_PUBLIC_BASE_URL="http://CHANGE_ME_NAS_IP:9898"
PKGSENDER_PS5_IP="CHANGE_ME_PS5_IP"
PKGSENDER_PS5_PORT="12800"
EOS
cp "$VAR_DIR/config.env" "$TMP/legacy-config.snapshot"
sh -eu -c '
  . "$1"
  load_config
  [ "$PKGSENDER_PS5_IP" = "" ]
  [ "$PKGSENDER_PUBLIC_BASE_URL" = "" ]
  [ "$PKGSENDER_PACKAGE_DIR" = "$VAR_DIR/packages" ]
' sh "$TMP/start-functions.sh"
cmp "$VAR_DIR/config.env" "$TMP/legacy-config.snapshot"
printf 'legacy placeholder upgrade without rewrite: PASS\n'

mkdir -p "$TMP/custom-library"
cat > "$VAR_DIR/config.env" <<EOS
PKGSENDER_PACKAGE_DIR="$TMP/custom-library"
PKGSENDER_PUBLIC_BASE_URL="http://192.168.1.8:9898"
PKGSENDER_PS5_IP="192.168.1.9"
PKGSENDER_LISTEN=":9898"
PKGSENDER_PS5_PORT="12800"
EOS
cp "$VAR_DIR/config.env" "$TMP/custom-config.snapshot"
sh -eu -c '
  . "$1"
  load_config
  [ "$PKGSENDER_PACKAGE_DIR" = "$2" ]
  [ "$PKGSENDER_PUBLIC_BASE_URL" = "http://192.168.1.8:9898" ]
  [ "$PKGSENDER_PS5_IP" = "192.168.1.9" ]
' sh "$TMP/start-functions.sh" "$TMP/custom-library"
cmp "$VAR_DIR/config.env" "$TMP/custom-config.snapshot"
printf 'existing settings preserved: PASS\n'

cat > "$VAR_DIR/config.env" <<EOS
PKGSENDER_PACKAGE_DIR="$TMP/nonexistent-primary"
PKGSENDER_PACKAGE_DIRS='["$TMP/custom-library"]'
PKGSENDER_PUBLIC_BASE_URL=""
PKGSENDER_PS5_IP=""
PKGSENDER_LISTEN=":9898"
PKGSENDER_PS5_PORT="12800"
EOS
sh -eu -c '
  . "$1"
  load_config
  [ -n "$PKGSENDER_PACKAGE_DIRS" ]
  [ "$PKGSENDER_PS5_IP" = "" ]
' sh "$TMP/start-functions.sh"
printf 'multi-root preference: PASS\n'
echo 'DSM7 bootstrap regression: PASS'
