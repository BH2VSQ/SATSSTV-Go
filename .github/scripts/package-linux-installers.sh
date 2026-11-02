#!/usr/bin/env bash
set -euo pipefail
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT_DIR"
APP_NAME="SATSSTV-Go"
ARTIFACT="${ARTIFACT:-linux-amd64}"
ARCH="${DEB_ARCH:-amd64}"
VERSION="${PACKAGE_VERSION:-}"
VERSION="${VERSION#v}"
if [[ -z "$VERSION" ]]; then
  if [[ "${GITHUB_REF_TYPE:-}" == "tag" && "${GITHUB_REF_NAME:-}" =~ ^v?[0-9] ]]; then VERSION="${GITHUB_REF_NAME#v}"; else VERSION="1.0.0+${GITHUB_RUN_NUMBER:-1}"; fi
fi
GUI_BIN="build/bin/${APP_NAME}"
CLI_BIN="build/bin/sstv-go"
ICON="build/appicon.png"
for file in "$GUI_BIN" "$CLI_BIN" "$ICON" README.md LICENSE; do [[ -f "$file" ]] || { echo "Missing required file: $file" >&2; exit 1; }; done
mkdir -p artifacts
WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

# Debian package
DEB_ROOT="$WORK/deb"
install -Dm755 "$GUI_BIN" "$DEB_ROOT/usr/bin/satsstv-go"
install -Dm755 "$CLI_BIN" "$DEB_ROOT/usr/bin/sstv-go-cli"
install -Dm644 "$ICON" "$DEB_ROOT/usr/share/icons/hicolor/1024x1024/apps/satsstv-go.png"
install -Dm644 README.md "$DEB_ROOT/usr/share/doc/satsstv-go/README.md"
install -Dm644 LICENSE "$DEB_ROOT/usr/share/doc/satsstv-go/copyright"
cat > "$DEB_ROOT/usr/share/doc/satsstv-go/RUNTIME.txt" <<'RUNTIME'
The desktop application requires GTK 3 and WebKit2GTK 4.1 at runtime.
The Debian package declares these dependencies for automatic installation.
RUNTIME
mkdir -p "$DEB_ROOT/usr/share/applications" "$DEB_ROOT/DEBIAN"
cat > "$DEB_ROOT/usr/share/applications/satsstv-go.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=SATSSTV-Go
GenericName=Slow Scan Television
Comment=Amateur radio SSTV transmission and reception
Exec=satsstv-go
Icon=satsstv-go
Terminal=false
Categories=AudioVideo;Audio;Utility;
StartupNotify=true
DESKTOP
cat > "$DEB_ROOT/DEBIAN/control" <<CONTROL
Package: satsstv-go
Version: ${VERSION}
Section: hamradio
Priority: optional
Architecture: ${ARCH}
Maintainer: BH2VSQ
Depends: libgtk-3-0 | libgtk-3-0t64, libwebkit2gtk-4.1-0, libasound2t64 | libasound2
Description: Amateur radio SSTV application
 SATSSTV-Go is a desktop and command-line application for slow-scan television.
CONTROL
dpkg-deb --root-owner-group --build "$DEB_ROOT" "artifacts/${APP_NAME}-${ARTIFACT}.deb"

# AppImage; GTK/WebKit are host runtime dependencies and are intentionally not bundled.
APPDIR="$WORK/${APP_NAME}.AppDir"
install -Dm755 "$GUI_BIN" "$APPDIR/usr/bin/${APP_NAME}"
install -Dm644 "$ICON" "$APPDIR/${APP_NAME}.png"
install -Dm644 /dev/stdin "$APPDIR/usr/share/doc/${APP_NAME}/RUNTIME.txt" <<'RUNTIME'
The desktop application requires GTK 3 and WebKit2GTK 4.1 at runtime.
Install the distribution packages libgtk-3-0 and libwebkit2gtk-4.1-0.
RUNTIME
cat > "$APPDIR/${APP_NAME}.desktop" <<'DESKTOP'
[Desktop Entry]
Type=Application
Name=SATSSTV-Go
GenericName=Slow Scan Television
Comment=Amateur radio SSTV transmission and reception
Exec=SATSSTV-Go
Icon=SATSSTV-Go
Terminal=false
Categories=AudioVideo;Audio;Utility;
StartupNotify=true
DESKTOP
cat > "$APPDIR/AppRun" <<'APPRUN'
#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
exec "$HERE/usr/bin/SATSSTV-Go" "$@"
APPRUN
chmod 755 "$APPDIR/AppRun"
TOOL="$WORK/appimagetool-x86_64.AppImage"
curl --fail --location --retry 3 https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-x86_64.AppImage --output "$TOOL"
chmod +x "$TOOL"
ARCH=x86_64 "$TOOL" --appimage-extract-and-run "$APPDIR" "artifacts/${APP_NAME}-${ARTIFACT}.AppImage"
chmod +x "artifacts/${APP_NAME}-${ARTIFACT}.AppImage"
echo "Created Linux installers:"
ls -lh "artifacts/${APP_NAME}-${ARTIFACT}.deb" "artifacts/${APP_NAME}-${ARTIFACT}.AppImage"
