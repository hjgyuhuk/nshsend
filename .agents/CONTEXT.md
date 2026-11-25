---
name: context
description: Static facts of this project — goal, global invariants, and environment boundaries valid for months. Update only when a durable fact changes. Never add tasks, progress, temporary rules, or file/line locations here.
---

# Goal
- Build `nshsend`, a LocalSend Protocol v2.2 compatible desktop LAN file and text transfer application, pairing a zero-dependency Go sidecar core (`core/`) with a Native Shell (NSH) TSX desktop UI (`app/`) over `stdin`/`stdout` NDJSON IPC.

# Invariants
- Go core `stdout` is strictly reserved for NDJSON IPC frames; diagnostic logs go to `stderr` and rotating `core.log` only.
- Over HTTPS, peer fingerprints come from the verified TLS certificate DER (uppercase SHA-256 hex) and must match claimed payload fingerprints; once a peer is verified over HTTPS, plain HTTP observations for that fingerprint are ignored.
- Only one incoming transfer session (`pending` or `active`) may run at a time (HTTP 409 on conflict; pending slots release on sender disconnect or `/cancel`).
- Acyclic vertical-slice boundaries:
  - Go `core/internal/shared` has zero I/O and imports no `internal/*` packages.
  - Go `discovery`, `receive`, and `send` never import `ipc` or each other.
  - Go `diaglog` and `desktopdialog` import no business domains.
  - TSX slices (`receive`, `send`, `history`, `settings`) never import each other's internal files or `app/App.tsx`.
  - TSX `app/src/ui/` never imports domain slices and uses semantic theme tokens only.
- NSH TSX components run once (Solid-style reactivity): never destructure reactive props; read signals inside JSX expressions/bindings and use global `<If>` / `<Each>`.
- All user-facing UI text comes from `t()` (`nsh/i18n`) backed by `locales/app.yml` (zh-CN + en); call `t` at the display site so it stays reactive. Interface-language choice (`settings/language.ts`) is applied with `setLocale` and persisted in `Storage`. Go-core technical error strings are passed through untranslated.

# Environment & Boundaries
- Desktop-first scope (Linux, macOS, Windows); the `process.spawn` sidecar architecture does not run on iOS, Android, or Web.
- Closing the main window hides it to the tray (`Application.setQuitMode("explicit")`); the app keeps receiving until the tray menu's quit item. Linux tray is a StatusNotifierItem and needs an SNI host panel (Plasma, waybar; GNOME needs AppIndicator).
- Go core builds with `CGO_ENABLED=0` and standard library only (zero third-party `go.mod` dependencies).
- UI requires `nsh` CLI (`>=0.7`), with GTK 4.14+ on Linux; Linux native file/folder dialogs prefer the xdg-desktop-portal FileChooser via a minimal stdlib-only D-Bus client in `desktopdialog` (the calling connection must stay alive until Response), falling back to `zenity` or `kdialog`; path open/reveal uses `xdg-open`.
- `nsh.json` declares explicit `capabilities`; `capabilities.process.execute` accepts bare executable names or absolute paths only.
- Linux distribution is a self-contained AppImage built by `scripts/package-appimage.sh`: `nsh package --target linux --format appimage` bundles the TSX app + NSH runtime but not the Go sidecar, so the script copies `bin/nshsend-core` into the AppDir's `usr/bin/` and prepends it to `PATH` in `AppRun`, then re-runs `appimagetool`. `process.spawn("nshsend-core")` resolves the bundled binary via the inherited `PATH`. Requires `appimagetool` (auto-downloaded to `bin/`) and system GTK 4 / WebKitGTK at runtime.
- macOS distribution is a self-contained `.app` built by `scripts/package-mac.sh` (must run on a Mac): `nsh package --target macos` bundles the TSX app + NSH runtime but not the Go sidecar. Bare `process.spawn` names resolve through the host `PATH` only and a Finder-launched app inherits launchd's minimal `PATH`, so the script copies `bin/nshsend-core` into `Contents/MacOS/`, renames the runner to `NshSend-bin`, and makes the main executable a wrapper that prepends its own directory to `PATH` before `exec`'ing the runner (the runner still locates `Contents/Resources/nsh` via `current_exe()`), then re-signs ad hoc (`codesign --force --deep --sign -`). Ad hoc runs only on the building Mac; Developer ID signing/notarization is not handled.
- Verification commands:
  - `./scripts/build-core.sh`
  - `cd core && go vet ./... && go test -race -timeout 30s -v ./...`
  - `nsh check`
  - `nsh test`
  - `./scripts/package-appimage.sh` (Linux AppImage)
  - `./scripts/package-mac.sh` (macOS .app, on a Mac)
