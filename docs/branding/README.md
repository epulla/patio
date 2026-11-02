# Patio brand assets and Wails icons

The illustrations use wax-crayon strokes. The lowercase **patio** wordmark keeps its original smooth, deep-plum lettering. The character and courtyard establish a shared visual style; public copy does not introduce or name a particular persona.

## Asset map

Paths below are relative to the repository root.

| File | Use | Size |
| --- | --- | --- |
| `docs/assets/patio-crayon-logo.png` | Transparent character and wordmark logo | 1774 × 887 |
| `docs/assets/patio-crayon-readme.png` | README header | 1983 × 793 |
| `docs/assets/patio-crayon-social.png` | Square social post | 1254 × 1254 |
| `docs/assets/patio-crayon-appicon-master.png` | Original square app-icon artwork | 1254 × 1254 |
| `build/appicon.png` | Wails app icon (macOS squircle grid, no border) | 1024 × 1024 RGBA PNG |
| `build/darwin/icons.icns` | macOS app bundle icon, generated from `build/appicon.png` | Multiple resolutions |
| `build/windows/icon.ico` | Windows executable icon | 16, 32, 48, 64, 128, 256 px |
| `packaging/icons/patio.icns` | Standalone macOS export | Multiple resolutions, up to 1024 px |
| `packaging/linux/patio.png` | Linux launcher icon | 512 × 512 |
| `packaging/linux/patio.desktop` | Linux launcher entry | Text file |

The app icon is a separate head-and-shoulders composition on a rounded, warm yellowish-brown paper-textured tile with an orange-brown border and transparency outside the tile. It keeps the face large enough for small launcher sizes. The full logo has transparency and is best used on a light background. Keep the dark wordmark on a cream or other light backing when displaying it on a dark page.

Revision 3 redraws the hand and paper plane together from a photographic pose reference in the logo and social image. The approved warm, bordered app icon and all native icon exports are preserved exactly. The README banner is unchanged. Earlier editions remain available in their previously delivered archives.

## Native icons in the build

The primary source is `build/appicon.png`, embedded by `main.go` for the Linux window icon and the app's About box. Generated assets are committed, so builds need no Wails CLI:

| File | Used by | Regenerate with |
| --- | --- | --- |
| `build/darwin/icons.icns` | `make app` copies it into `patio.app/Contents/Resources` (`CFBundleIconFile` in `build/darwin/Info.plist`) | `wails3 generate icons -input build/appicon.png -macfilename build/darwin/icons.icns -windowsfilename "$(mktemp)"` |
| `rsrc_windows_amd64.syso` | Go links it into `patio.exe`: icon, per-monitor DPI and app manifest | `wails3 generate syso -arch amd64 -icon build/windows/icon.ico -manifest build/windows/patio.exe.manifest -info build/windows/info.json -out rsrc_windows_amd64.syso` |

Install the CLI with `go install github.com/wailsapp/wails/v3/cmd/wails3@v3.1.0`. The icons command above sends its ICO to a temporary file so the hand-made `build/windows/icon.ico` stays unchanged; when the design changes, replace that ICO too and regenerate the syso.

`build/appicon.png` is the tile interior without the orange border, clipped to an 824 px rounded square centered on the 1024 px canvas (Apple's icon grid); macOS 26 otherwise shrinks the icon onto a dark "squircle jail" plate. Windows and Linux launcher icons keep the bordered tile. `packaging/icons/patio.icns` is a standalone export, not a build input.

### Linux

`main.go` sets the window icon from the embedded PNG, `ProgramName: "patio"`, and keeps `WebviewGpuPolicyNever`.

Desktop-menu and GNOME Wayland icon handling also needs an installed desktop entry and icon. The release workflow publishes a bare Linux executable; it does not install these files automatically.

For a user-local installation after `make linux`:

```sh
install -Dm755 build/bin/patio "$HOME/.local/bin/patio"
install -Dm644 packaging/linux/patio.png "${XDG_DATA_HOME:-$HOME/.local/share}/icons/hicolor/512x512/apps/patio.png"
install -Dm644 packaging/linux/patio.desktop "${XDG_DATA_HOME:-$HOME/.local/share}/applications/patio.desktop"
```

The desktop entry uses `Exec=patio`. Ensure `$HOME/.local/bin` is on the desktop session's PATH, or set `Exec` to the actual absolute executable path before installing the entry. Confirm launcher appearance and window grouping on the target desktop.

## Export and validation

The artwork was created with the built-in image-generation tool. The [generation notes](GENERATION.md) preserve the final prompts. Resizing is a format-preparation step; the ICO and ICNS use the same encoder versions as Wails v2.16.0:

- `github.com/leaanthony/winicon v1.0.0`
- `github.com/jackmordaunt/icns v1.0.0`

Validation checks the image dimensions, readable native icon entries, alpha where intended, Git ignore behavior, and Go formatting. A small compile fixture checks the Linux options against Wails v2.16.0. A full Patio desktop build and on-device icon smoke tests were not run.

## Source references

- [Wails v3.1.0 icon and syso generation](https://github.com/wailsapp/wails/tree/v3.1.0/v3/internal/commands)
- [Wails v3.1.0 Linux window options and GPU policy](https://github.com/wailsapp/wails/blob/v3.1.0/v3/pkg/application/webview_window_options.go)
