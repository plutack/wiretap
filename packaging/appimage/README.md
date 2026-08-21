# AppImage packaging

Inputs for building `wiretap-<ver>-<arch>.AppImage`.

## Files

- `../linux/wiretap.svg` - shared vector icon. The build rasterizes this to `256x256` and `512x512` PNGs into `AppDir/usr/share/icons/hicolor/<size>/apps/wiretap.png` and a top-level `.DirIcon`.
- `AppRun` — bash entry point. `make appimage` marks it `0755` at build time.

## Runtime requirements

The AppImage does not bundle GTK/WebKit. The user still needs:

- `webkit2gtk-4.1` and `gtk3` (Ubuntu/Debian: `webkit2gtk-4.1`, `libgtk-3-0`; Fedora: `webkit2gtk6`, `gtk3`; Arch: `webkit2gtk-4.1`, `gtk3`).

## Build tools

CI downloads `appimagetool` from AppImageKit at build time and rasterizes the icon with `rsvg-convert` (`librsvg2-bin`). `make appimage` accepts `rsvg-convert`, `magick`, or `convert` locally, and fetches `appimagetool` into `tools/` on first run.
