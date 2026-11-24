#!/bin/sh
# Verify package replacement on an explicitly selected disposable emulator.
set -eu

if [ "$#" -ne 3 ]; then
    echo "Usage: $0 EMULATOR_SERIAL PREVIOUS_APK NEXT_APK" >&2
    exit 2
fi
serial=$1
previous=$2
next=$3
package=org.polysync.android.preview
tools_dir=${ANDROID_HOME:?Set ANDROID_HOME}/build-tools/35.0.0
adb="$ANDROID_HOME/platform-tools/adb"
target() { "$adb" -s "$serial" "$@"; }
fail() { echo "$*" >&2; exit 1; }

[ "$(target shell getprop ro.kernel.qemu | tr -d '\r')" = 1 ] || fail 'Select a disposable Android emulator.'
version_code() {
    metadata=$("$tools_dir/aapt" dump badging "$1") || return 1
    printf '%s\n' "$metadata" | awk -F"'" -v app="$package" 'NR == 1 && $2 == app {print $4}'
}
certificate() {
    verified=$("$tools_dir/apksigner" verify --print-certs "$1") || return 1
    printf '%s\n' "$verified" | sed -n 's/^Signer #1 certificate SHA-256 digest: //p'
}
old_code=$(version_code "$previous")
new_code=$(version_code "$next")
[ -n "$old_code" ] && [ -n "$new_code" ] && [ "$new_code" -gt "$old_code" ] || fail 'Expected two preview APKs with increasing version codes.'
old_cert=$(certificate "$previous")
[ -n "$old_cert" ] && [ "$old_cert" = "$(certificate "$next")" ] || fail 'Preview certificate changed.'

installed_code() {
    target shell dumpsys package "$package" | sed -n 's/^ *versionCode=\([0-9]*\).*/\1/p' | head -n 1
}
snapshot() {
    # The tree includes the identity, pairings, member grants, workspace and transfer files.
    # Stop the service first so a concurrent sync cannot change these files during replacement.
    target shell -T run-as "$package" sh <<'DEVICE'
set -eu
for tree in files/polysync shared_prefs; do
    if [ -d "$tree" ]; then
        find "$tree" -type f -exec sha256sum {} \;
    fi
done
DEVICE
}

target install -r "$previous"
[ "$(installed_code)" = "$old_code" ] || fail 'Previous APK installation failed.'
target shell am force-stop "$package"
uid=$(target shell run-as "$package" id -u)
marker="files/.polysync-upgrade-$$"
target shell run-as "$package" mkdir -p files
# Refuse to overwrite any existing test data at this location.
if target shell run-as "$package" test -e "$marker"; then fail 'Marker already exists.'; fi
trap 'target shell run-as "$package" rm -f "$marker"' 0
printf '%s' "$marker" | target shell -T run-as "$package" tee "$marker" >/dev/null
before=$(snapshot)
target install -r "$next"
[ "$(installed_code)" = "$new_code" ] || fail 'New APK installation failed.'
[ "$uid" = "$(target shell run-as "$package" id -u)" ] || fail 'App UID changed.'
[ "$(target exec-out run-as "$package" cat "$marker")" = "$marker" ] || fail 'Private data marker was lost.'
after=$(snapshot)
[ "$(printf '%s\n' "$before" | LC_ALL=C sort)" = "$(printf '%s\n' "$after" | LC_ALL=C sort)" ] || fail 'App-private files changed during replacement.'
echo "Preview upgrade $old_code -> $new_code kept the app UID and private data."
