#!/usr/bin/env sh
set -eu
PROJECT_ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$PROJECT_ROOT"
TOOLS="$PROJECT_ROOT/.cache/android-tools"
export ANDROID_HOME="${ANDROID_HOME:-$TOOLS/sdk}"
export ANDROID_NDK_HOME="${ANDROID_NDK_HOME:-$ANDROID_HOME/ndk/28.2.13676358}"
if [ -z "${JAVA_HOME:-}" ]; then JAVA_HOME=$(find "$TOOLS/jdk" -mindepth 1 -maxdepth 1 -type d | head -n 1); export JAVA_HOME; fi
export GOCACHE="${GOCACHE:-$PROJECT_ROOT/.cache/go-build-linux}" GOMODCACHE="${GOMODCACHE:-$PROJECT_ROOT/.cache/go-mod}"
export GRADLE_USER_HOME="${GRADLE_USER_HOME:-$PROJECT_ROOT/.cache/gradle-android}"
export PATH="$JAVA_HOME/bin:$PROJECT_ROOT/.cache/linux-toolchain/go/bin:$TOOLS/bin:$PATH"
GRADLE="${GRADLE:-$TOOLS/gradle-8.13/bin/gradle}"
mkdir -p apps/android/app/libs dist
gomobile bind -target=android/arm64 -androidapi=29 -javapkg=org.polysync.core -ldflags='-s -w' -o apps/android/app/libs/polysync.aar ./mobile
# Avoid incremental APK holes retaining bytes of superseded native libraries.
"$GRADLE" -p apps/android --no-daemon :app:clean :app:testDebugUnitTest :app:assembleDebug :app:assembleDebugAndroidTest :app:lintDebug
cp apps/android/app/build/outputs/apk/debug/app-debug.apk dist/polysync-android-arm64-preview.apk
echo "Built dist/polysync-android-arm64-preview.apk (debug signing; no store upload)"
