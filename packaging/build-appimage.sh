#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
version=${VERSION:-dev}
build_dir="$root/build/appimage"
appdir="$build_dir/AppDir"
tools="$build_dir/tools"
output="$root/dist/ps4pkginfo-${version}-x86_64.AppImage"

if [[ $(uname -m) != x86_64 ]]; then
	echo "AppImage build currently supports x86_64 only" >&2
	exit 1
fi

rm -rf "$appdir"
mkdir -p "$appdir/usr/bin" "$appdir/usr/share/ps4pkginfo" "$tools" "$root/dist"

blueprint-compiler compile "$root/data/window.blp" --output "$appdir/usr/share/ps4pkginfo/window.ui"
go build -trimpath -ldflags='-s -w' -o "$appdir/usr/bin/ps4pkginfo" "$root"
go build -trimpath -ldflags='-s -w' -o "$appdir/usr/bin/ps4pkginfo-gtk" "$root/cmd/ps4pkginfo-gtk"

linuxdeploy="$tools/linuxdeploy-x86_64.AppImage"
gtk_plugin="$tools/linuxdeploy-plugin-gtk.sh"
if [[ ! -x $linuxdeploy ]]; then
	curl -fL -o "$linuxdeploy" https://github.com/linuxdeploy/linuxdeploy/releases/download/continuous/linuxdeploy-x86_64.AppImage
	chmod +x "$linuxdeploy"
fi
if [[ ! -x $gtk_plugin ]]; then
	curl -fL -o "$gtk_plugin" https://raw.githubusercontent.com/linuxdeploy/linuxdeploy-plugin-gtk/7a3fbc31a9e5075073ff8790f26effbac5f84453/linuxdeploy-plugin-gtk.sh
	chmod +x "$gtk_plugin"
fi

plugin_args=()
pixbuf_modules=$(pkg-config --variable=gdk_pixbuf_moduledir gdk-pixbuf-2.0)
if [[ -d $pixbuf_modules ]]; then
	plugin_args=(--plugin gtk)
else
	echo "warning: GTK plugin skipped; this system has no GDK Pixbuf module directory" >&2
fi

rm -f "$output"
PATH="$tools:$PATH" APPIMAGE_EXTRACT_AND_RUN=1 LDAI_OUTPUT="$output" env -u VERSION "$linuxdeploy" \
	--appdir "$appdir" \
	--executable "$appdir/usr/bin/ps4pkginfo-gtk" \
	--desktop-file "$root/data/io.github.hauren.PS4PkgInfo.desktop" \
	--icon-file "$root/data/io.github.hauren.PS4PkgInfo.png" \
	"${plugin_args[@]}" \
	--output appimage

echo "$output"
