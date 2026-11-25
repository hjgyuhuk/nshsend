---
name: state
description: Current work state — focus, verified progress, blockers/unverified hypotheses, temporary guardrails for the active task, and next steps. Written as a handoff for the next session. Keep under ~100 lines: promote durable lessons to DECISIONS.md, delete superseded progress and expired guardrails.
---

# Now
- Focus: LocalSend Protocol v2.2 desktop MVP (`core/` + `app/`); Linux file/folder pickers now use xdg-desktop-portal via a stdlib-only D-Bus client.

# Progress
- [Done] Go sidecar core (`core/cmd/nshsend-core`, `core/internal/{shared,identity,discovery,receive,send,desktopdialog,diaglog,ipc}`) — verified by `cd core && go vet ./... && go test -race -timeout 30s -v ./...` and `./scripts/build-core.sh`.
- [Done] NSH TSX UI (`nsh.json`, `app/App.tsx`, `app/src/{ui,sidecar,receive,send,history,settings}`, `app/App.test.tsx`) — verified by `nsh check` (0 errors, 0 warnings) and `nsh test` (1/1 passed).
- [Done] Live transfer against an external LocalSend peer succeeded after adding unconditional `GetClientCertificate`; transient EOF auto-retry and `logs.tail` verified via automated tests.
- [Done] Linux portal pickers (`desktopdialog/dbus.go` + `portal.go`): live e2e on niri/Wayland returned real file and folder selections; session-bus fallback `/run/user/<uid>/bus` covers NSH's sanitized spawn env (verified with stripped env); ctx-cancel `Request.Close` verified live; zenity/kdialog remain fallbacks. Live cancel-by-user path unit-tested only.
- [Done] Sequential uploads (`send/session.go`): iOS LocalSend 1.19.0 resets concurrent upload connections (reproduced with folder and with two loose files); single-file over the reused prepare-upload connection always worked. Folder send verified live after the fix.
- [Done] ModalSheet centered via `m-auto` on the popup (NSH Dialog desktop popup/backdrop are siblings; backdrop flex-centering does not move the popup) — `nsh check`/`nsh test` pass; visual check pending app restart.
- [Done] Tray icon + menu (`app/tray.svg`, `NshSendApp` in `app/App.tsx`): close hides the window (`quitMode: "explicit"`, controlled `open` + `closeOnPress={false}`), tray click / 「显示主窗口」 restores it, `{ role: "quit" }` exits — `nsh check`/`nsh test` pass; live tray visibility pending (niri needs an SNI host panel such as waybar).
- [Done] Sidebar nav icons + roomier spacing (`app/icons/{receive,send,history,settings}.svg`, tinted via `Image` `tintColor` + `token()`; sidebar `p-4`, tab `py-3`, list `gap-2`) — `nsh check`/`nsh test` pass; visual check pending app restart.
- [Done] i18n (zh-CN + en): all UI text via `t()` from `locales/app.yml` (`_version: 2`); language picker in Settings (`settings/language.ts`, `setLocale` + `Storage` persistence, restored at startup); tray menu follows locale via `effect`; history time uses `toLocaleString(locale())`; replaced `msg().includes("PIN")` with a `sendPinRequired` signal. `nsh check` + `nsh test` (2/2, incl. a live zh-CN→en→zh-CN switch test) pass; visual check pending app restart.
- [Done] Linux AppImage packaging (`scripts/package-appimage.sh`, `icon.png`/`icon.svg`, `nsh.json` `icon`): bundles NSH runtime + Go sidecar by injecting `nshsend-core` into the AppDir `usr/bin/` and adding it to `PATH` in `AppRun`, then re-running `appimagetool`. Verified live: the packaged AppImage spawns the bundled core (`/proc/<pid>/exe` points inside the extracted AppDir) and it listens on :53317. Output `.nsh/out/NshSend-x86_64.AppImage`.
- [Done] macOS .app packaging (`scripts/package-mac.sh`): injects `nshsend-core` into `Contents/MacOS/` and wraps the runner (`NshSend-bin`) with a PATH-prepending main executable, then re-signs ad hoc. Verified live on Intel macOS: `open -n` (LaunchServices env) launches the app, the sidecar's executable path points inside the bundle (`lsof` txt) and listens on :53317; SIGTERM to the runner exits both cleanly. Output `.nsh/out/NshSend.app`.

# Blockers / unverified
- [Diagnosed, upstream] Linux 拖放文件进发送暂存区无效（niri 26.04 / Wayland / Nautilus 51.2 / gtk4 4.24.1 / nsh 0.7.0）。WAYLAND_DEBUG 实证：Nautilus 的 offer 含 `text/uri-list`、`source_actions(7)`，niri 正常派发 `wl_data_device.enter/motion` 进窗口；但 nsh GTK 宿主每次回 `set_actions(0,0)` + `accept(serial, nil)`，全程拒绝、无 drop 事件 → `onFileDrop` 从未触发。根因在 nsh 运行时 GTK `DropTarget`（`crates/nsh-platform-linux/src/file_drop.rs` 写死 `DragAction::COPY`，main 分支未改；疑似 COPY-only 动作协商或目标注册/命中问题），**非本仓库 TSX、非 niri**。应用代码已正确（`SendPage.tsx:63-69`）。当前替代入口：门户「选择文件/文件夹」按钮（已可用）。修复需上游 nsh，本机无 cargo 不能重编运行时。

# Next
- Run `./scripts/dev.sh` to validate the Settings diagnostic log viewer and EOF auto-retry interactively.
- Optional next milestones: Web Share download server, transfer speed indicator, per-file selective accept UI.
