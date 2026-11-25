# Linux (GTK): file drops from a file manager are silently rejected on Wayland (`onFileDrop` never fires)

## Summary

On Linux with the GTK host, dragging files from a file manager onto a view that
registers `onFileDrop` does nothing. The host rejects the drag at the GTK/Wayland
accept stage: it answers `wl_data_offer.accept(serial, nil)` and
`wl_data_offer.set_actions(0, 0)` on enter and on every motion, so no drop is ever
delivered and `onFileDragEnter` / `onFileDrop` never fire. The compositor and the
drag source behave correctly — the offer carries `text/uri-list` and enters the
window normally.

## Environment

- nsh: 0.7.0 (3f8ecd8), default Linux target (GTK host, `nsh-linux-host`)
- OS: CachyOS / Arch Linux, x86_64
- Compositor: niri 26.04 (Wayland session)
- GTK: gtk4 4.24.1
- Drag source: GNOME Nautilus 51.2 (GTK4, native Wayland)
- App: a TSX view with `onFileDragEnter` / `onFileDragLeave` / `onFileDrop`
  (reads `e.uris`). `nsh check` passes; the same handlers work via the test
  agent's `dropFiles`.

## Steps to reproduce

1. Run a TSX app whose view registers `onFileDrop` (`nsh run`, GTK host) under a
   Wayland compositor.
2. Drag a regular file from Nautilus over that view and release.
3. Nothing happens: no drag-enter highlight, no drop callback.

## Expected

The view accepts the file list and `onFileDrop` fires with the dropped `file:`
URIs, as it does on macOS and via the simulated `dropFiles` path.

## Actual / evidence

Captured with `NSH_VERBOSE=1 WAYLAND_DEBUG=1 nsh run`. The drag offer is valid and
the compositor delivers it into the app surface:

```
wl_data_offer#…offer("application/x-gtk-local-dnd")
wl_data_offer#…offer("text/plain;charset=utf-8")
wl_data_offer#…offer("text/uri-list")
wl_data_offer#…offer("application/vnd.portal.filetransfer")
wl_data_offer#…offer("application/vnd.portal.files")
wl_data_offer#…source_actions(7)                 # copy | move | ask
wl_data_device#31.enter(278862, wl_surface#40, 738.9, 140.1, wl_data_offer#…)
```

But the host rejects it on enter and on every motion, and there is no drop:

```
 -> wl_data_offer#…set_actions(0, 0)
 -> wl_data_offer#…accept(278862, nil)
wl_data_device#31.motion(…)
 -> wl_data_offer#…set_actions(0, 0)
 -> wl_data_offer#…accept(278862, nil)
… (repeats for the whole drag) …
wl_data_device#31.leave()
 -> wl_data_offer#…destroy()
```

No `accept` ever names a mime type, `set_actions` is always `(0, 0)`, and no
`wl_data_device.drop` / `wl_data_offer.receive` occurs. So the rejection is on the
destination (GTK host) side, not the compositor or the source.

## Likely cause

`crates/nsh-platform-linux/src/file_drop.rs`, `install()`:

```rust
let controller = gtk::DropTarget::new(gdk::FileList::static_type(), gdk::DragAction::COPY);
```

The drop target advertises only `COPY`. On Wayland the action is negotiated
between source, destination and compositor; a `COPY`-only target can end up
advertising no usable action (`set_actions(0, 0)`) and rejecting the offer
(`accept nil`). A similar GTK4 app hit exactly this with Nautilus-on-Wayland and
fixed it by accepting more than `COPY` (and merging competing drop targets /
formats); see the pixstrip commit "Fix drag-and-drop and file manager integration"
(`DropTarget only accepted COPY action, but Wayland compositor pre-selects MOVE,
causing GTK4 to silently reject all drops`).

Note: `main` still has `DragAction::COPY` here.

## Suggested fix

- Accept `DragAction::COPY | DragAction::MOVE` (and negotiate accordingly) instead
  of `COPY` only, and verify the resulting `set_actions` / `accept` against a
  Nautilus drag on Wayland.
- Confirm the destination registration / hit-testing path actually installs the
  controller for a view that registers `onFileDrop` (the `accept nil` +
  `set_actions(0, 0)` pattern is also what GTK emits when no matching drop target
  is present).

## Notes

- The app-side code is correct and unchanged; the simulated `dropFiles` path
  (which bypasses GTK negotiation) works, which is why this only shows up with a
  real file-manager drag on Wayland.
- Happy to provide the full `WAYLAND_DEBUG` log or test a patch on niri/GTK4.