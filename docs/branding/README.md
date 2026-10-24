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
| `build/windows/icon.ico` | Windows executable icon | 16, 32, 48, 64, 128, 256 px |
| `packaging/icons/patio.icns` | Standalone macOS export | Multiple resolutions, up to 1024 px |
| `packaging/linux/patio.png` | Linux launcher icon | 512 × 512 |
| `packaging/linux/patio.desktop` | Linux launcher entry | Text file |

The app icon is a separate head-and-shoulders composition on a rounded, warm yellowish-brown paper-textured tile with an orange-brown border and transparency outside the tile. It keeps the face large enough for small launcher sizes. The full logo has transparency and is best used on a light background. Keep the dark wordmark on a cream or other light backing when displaying it on a dark page.

Revision 3 redraws the hand and paper plane together from a photographic pose reference in the logo and social image. The approved warm, bordered app icon and all native icon exports are preserved exactly. The README banner is unchanged. Earlier editions remain available in their previously delivered archives.

## Use the assets in another Patio checkout

Extract the files into the root of the Patio repository, preserving their folders. The included `README.md` references the new banner.

The working checkout used to prepare these files already contains the icon integration. To apply that integration to a different checkout, inspect and apply the supplied patch:

```sh
git apply --check docs/branding/wails-icons.patch
git apply docs/branding/wails-icons.patch
wails build
```

The patch changes only `.gitignore` and `main.go`. It was prepared against commit `eb06db484cfb9a2d5187095208c8cda172154df3`. If those files have changed in your checkout, review the small patch and apply the equivalent changes manually. The PNG and ICO must be present before compiling.

### Windows and macOS

For this repository's Wails v2.16.0 configuration, the primary source is `build/appicon.png`. Windows embeds `build/windows/icon.ico`. Both are deliberately allowed by the updated Git ignore rules, so they can be included in source control and release builds.

Wails preserves an existing Windows ICO. When updating the design in the future, replace or regenerate the matching ICO along with the PNG; changing only the PNG leaves an existing Windows ICO unchanged. A clean build does not remove that override.

On macOS, Wails generates `iconfile.icns` inside the application bundle from `build/appicon.png`. That PNG is the tile interior without the orange border, clipped to an 824 px rounded square centered on the 1024 px canvas (Apple's icon grid). macOS 26 otherwise shrinks the icon onto a dark "squircle jail" plate. Windows and Linux launcher icons keep the bordered tile. The supplied `packaging/icons/patio.icns` is a convenient standalone export; it is not an input override for Wails and does not need to be copied to `build/darwin/`.

After integrating the assets, use the repository's normal build:

```sh
wails build
```

### Linux

The patch embeds the PNG in `main.go` and supplies `linux.Options.Icon`, with `ProgramName: "patio"`. It explicitly keeps `WebviewGpuPolicyNever`, preserving the previous default rendering policy.

Desktop-menu and GNOME Wayland icon handling also needs an installed desktop entry and icon. The current release workflow publishes a bare Linux executable; it does not install these files automatically.

For a user-local installation after building on Linux:

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

Validation checks the image dimensions, readable native icon entries, alpha where intended, patch applicability, Git ignore behavior, and Go formatting. A small compile fixture checks the Linux options against Wails v2.16.0. A full Patio desktop build and on-device icon smoke tests were not run.

## Source references

- [Wails v2.16.0 icon generation and platform packaging](https://github.com/wailsapp/wails/blob/v2.16.0/v2/pkg/commands/build/packager.go)
- [Wails v2.16.0 Linux options and GPU default](https://github.com/wailsapp/wails/blob/v2.16.0/v2/pkg/options/linux/linux.go)
- [Wails application options](https://v2.wails.io/docs/reference/options/)

