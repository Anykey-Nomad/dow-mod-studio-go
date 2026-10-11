# DoW Mod Studio

> English | [Русский](README_ru.md)

A desktop tool (Go + Gio UI) for modding **Warhammer 40,000: Dawn of War — Definitive Edition**.
It opens `.module` files, builds a single merged tree of the mod's files from every source (the mod itself,
required mods, engine `.sga` archives) and lets you view, listen to, edit and repack their contents — from
`.rgd` unit tables to `.whm` 3D models, `.whe` effects and `.fda` audio.

The program reads and writes the game's formats directly: saves go to the mod's own folder (overlay), packing
writes SGA v2 archives following Relic's `CSgaCreator`, and cross-file references are indexed and checked
for breakage.

## Features

* Viewing and editing RGD tables (units, buildings, weapons, abilities, squads) with reference navigation.
* The **Object** window — an editor for the mod's objects: unit properties, weapons, abilities, squads,
  relationships, broken references, effect timelines and a 3D viewport with animation and bone editing.
* A 3D viewer for `.whm`: skeleton, animations, markers, `.whe` FX and sound preview, export to `.sgm`.
* An audio player for `.fda` and `.wav` with a waveform, seeking and export to WAV.
* An image viewer (`.dds`, `.rsh`, `.rtx`, `.tga`, `.png`, `.jpg`, `.bmp`) with mip levels.
* A syntax-highlighting text editor for `.lua`, `.txt`, `.ai`, `.scar`, `.rat`, `.con` and more (list configurable).
* Localization: language selection, `$id` resolution against `.ucs` tables, jump-to-string and text editing.
* Freeze/Unfreeze of units and buildings with a written rollback journal.
* Packing the mod into a `.sga` (SGA v2) and unpacking archives for inspection.
* Filename search with type filters, recent-modules history, a recycle bin, and session autosave.

## System requirements

* Windows (primary platform: Direct3D 11 / Vulkan rendering, `.module` association). On other OSes the
  program builds and runs on the software renderer, without hardware 3D and without file association.
* An installed copy of **Dawn of War Definitive Edition** (the standard Steam paths are pre-filled in the
  open-module dialog).

## Quick start

1. Run `dow-mod-studio.exe` (or build it — see [Building](#building-from-sources)).
2. `File → Open Module…` — type a game folder or pick a `.module` from the list (the dialog scans the folder
   for `.module` files).
3. The mod tree appears on the left and the **Module** tab opens automatically. Double-clicking a file opens
   a tab with the viewer that matches its type.
4. The **Object** button in the top right opens the object window — the main editor for units and buildings.
5. `Mod → Save` writes accumulated RGD/text edits into the mod's folder; `Mod → Pack SGA…` builds an archive.

## Interface

The window consists of:

* **Menu bar** (top): the `File`, `Mod` and `Help` menus, and on the right — a language picker, the **Object**
  button and a search field.
* **Tab bar**: the `Module` tab (the mod tree) plus the open file tabs. The wheel scrolls the bar sideways,
  right-clicking a tab opens a context menu ("close all tabs"), and the ✕ closes a tab.
* **Three-panel area**: the mod tree on the left, the content in the middle (RGD tree / player / model), and
  a **Properties** panel on the right. The panels are resizable and their widths are remembered.
* **Status bar** (bottom): the result of the last action, the path of the open file, and errors.

The **Module** tab and the file tabs are independent: the mod tree stays in place while you switch between
open files.

---

## File menu

| Item | What it does |
|---|---|
| `New Mod…` | Placeholder: prints a message in the status bar (mod creation from a template is not implemented yet). |
| `Open Module…` | The `.module` dialog. Scans: the executable's folder, both standard Steam Definitive Edition paths, the current module's folder, recent folders, and a manually typed folder. A typed directory is a scan request; only a `.module` file is loaded. |
| `Close` | Closes all tabs and unloads the mod, releasing its file watcher and 3D resources. |
| `Settings` | The settings dialog (see below). |
| `Associate .module files` / `Remove .module association` | (Windows) registers/removes the `.module` file association for the current user — afterwards any `.module` opens this program on a double-click in Explorer. |
| `Recent Modules` | An expandable list of the last modules (up to 8); clicking one opens it. |
| `View Recycle Bin` | Opens the recycle-bin folder in Explorer. |
| `Empty Recycle Bin` | Empties the recycle bin (with confirmation). |
| `Exit` | Quits. |

## Mod menu

| Item | What it does |
|---|---|
| `Save` | Writes the active tab's accumulated edits (RGD or text) to the mod's folder through the overlay: the file lands in the current module's `Data\…` folder and the tree refreshes itself. |
| `Export File` | Copies a file from an SGA archive or a parent mod into the current mod's folder, making it "yours" and editable. Refused when the file already exists in the mod folder. |
| `Pack SGA…` | Packs all files of the current mod into an SGA v2 archive (`<ModFolder>\<FileMap>.sga`, or `…_packed.sga` if one exists) and adds the archive entry to the `.module`. |
| `Extract SGA…` | A dialog listing the mod's SGA archives: unpacks the selected ones for viewing/editing. |
| `Burn Mod` | Placeholder (prints a message in the status bar for now). |
| `Module Settings…` | Placeholder (prints a message in the status bar for now). |

## Help menu

| Item | What it does |
|---|---|
| `About DoW Mod Studio`, `Documentation`, `Check for Updates` | Placeholders: print a message in the status bar. |
| `Open log folder` | Opens the folder with the program's log. |

## Right-hand end of the menu bar

* **Language picker** — a dropdown of languages found in the `.ucs` string tables (game folder and module
  folder). The language is a global setting: it resolves every `$id` localized string in RGD files and the
  object window.
* **Object** — a button that opens the object window (see below). Highlighted while the window is open.
* **Search** — a filename search field with a results dropdown over the tree. Filter chips: `All`, `RGD`,
  `Lua`, `FDA`, `ART` (textures: `.art/.rsh/.dds/.tga/.texture`), `WHM`. Arrow keys and Enter pick a hit;
  clicking opens the file. Retyping re-searches; a dismissed panel stays down until the query actually changes.

## The mod tree (left panel)

One merged tree of every file the mod can reach. The row color shows the source:

* **red** — files of the loaded mod itself;
* **blue** — files of dependent (required) mods;
* **grey** — engine files (the game's archives).

Functions:

* folders expand and collapse; arrow keys navigate, `Enter` opens a file, `Delete` deletes (with confirmation;
  only a file of the current mod can be deleted);
* right-clicking a file shows a menu: **Duplicate** (a copy inside the mod) and **Delete** (to the recycle bin);
* deleted files go to the recycle bin at `%AppData%\DoW Mod Studio\recycle`, from where they can be restored;
* the tree auto-refreshes when the mod's files change on disk (fsnotify) — no reload needed;
* references in an RGD that resolve to nothing are tinted red.

## File tabs and viewers

The tab type is chosen by file extension:

| Format | Tab | Capabilities |
|---|---|---|
| `.rgd` | RGD editor | A tree of the table on the left (collapsing, arrow-key navigation) and **Properties** on the right: scalar values (numbers, strings, bool), references to other `.rgd` files (Enter jumps to the file), value suggestions. `Alt`+click on a localized string (`$id`) opens it in the `.ucs`. Edits accumulate until `Mod → Save`. |
| `.lua`, `.txt`, `.ai`, `.scar`, `.rat`, `.con`, … | Text editor | Syntax highlighting, a dirty flag, saving through `Mod → Save`. The extension list is configurable in Settings; files of other naming from that list open here too. |
| `.ucs` | Text editor (read-only) | Localization files live outside the mod, so the tab is read-only and says so. Opened from the localization picker. |
| `.fda` | Audio player | Play/Pause, stop, ±10/±30 s seeking, a position slider, repeat, volume 0–200 %, a clickable waveform, `Export WAV`. FDA properties: sample rate, channels, duration. |
| `.wav` | Audio player | Play/Stop, repeat, volume, waveform, conversion/export. |
| `.jpg/.jpeg/.png/.bmp/.tga/.dds/.rtx/.rsh` | Image viewer | Zoom, fit, mip-level stepping (DDS/RSH/RTX), image properties on the right. |
| `.whm` | 3D viewer | The model with its `.rsh` materials, an animation list with playback (play/pause, step frame back/forward, reset), skeleton, textures, zoom, UV rotate/mirror, preview of the model's `.whe` effects and sounds (the FX button, effect scale, X/Y/Z heading angles), and a `Convert to SGM` button that exports the model to `.sgm`. |

Every tab has a **Properties** panel on the right: file details, FDA/WAV/image/model properties, or the
properties of the selected RGD node plus **Table Children**.

## The Object window (the "Object" button)

The main editor for the mod's objects. At the top — race and unit pickers (with icons and localized names),
the **New Unit** and **New Building** buttons (write a new `.rgd` from a template modeled on the game's own
files, straight into the mod — no reload), and **Freeze** / **Unfreeze**. On the left is the section list, in
the middle the section's content, on the right the properties of the selected row.

Sections:

* **Unit info** — the scalar properties of the unit/building (health, armor, cost, icons, localized names …).
  A row is edited in the right column (Save / Revert / Open RGD); localized values resolve in the language
  picked in the menu bar.
* **Weapons** — the unit's weapons, with navigation to each one's properties.
* **Abilities** — the unit's abilities, likewise.
* **Squads** — the squads the unit belongs to (the link is built by a background scan of every squad file),
  with navigation to the squad file.
* **Links** — both directions of the relationships: above the divider what this object refers to (weapons,
  abilities, the building that produces it …), below it what refers to it. Role, slot, target; unresolved
  references are marked red. Freeze reads exactly this list.
* **Frozen** — units and buildings moved out of the mod into the project; shows what was cleared and where,
  and allows bringing them back (Unfreeze).
* **Problems** — every broken reference in the mod: references naming a file that is not there.
* **Events** — the selected model's `.whe` timelines: event definitions (DATAEVNT) and the timelines (DATAMTON)
  that fire them. A form for adding an event (name, moment, FX, sound, marker), editing event fields (Enter or
  Set), Copy/Paste through a clipboard history, removal, and saving the `.whe` (Save/Revert). Audition: one
  event's sound on its own, or the timeline's sounds in order with a playhead and scrubbing; FX are redrawn
  in the viewport.
* **Viewport** — the unit's 3D model: animation list, cameras, playback, marker and bone lists. Bone keyframe
  editing: adding position/rotation keys on the playhead, inserting a key before/after the selection, deleting,
  shifting by frames, mirroring on the X/Y/Z axes, −180…180° angle sliders, Undo, and writing back to the
  `.whm` (Save). "Import frame's timeline" copies keys from another `.whm`'s timeline (Copy/Replace).

## Freeze / Unfreeze

**Freeze** moves a unit or building out of the mod without deleting it: its `.rgd` files are moved to the
project folder (`%AppData%\DoW Mod Studio\Projects\<Mod>\Units`), the game no longer sees them, but the files
are kept for inspection. Before moving anything, the program finds every reference to the entity (a weapon,
for instance, may be used by hundreds of units), clears them, and records the original values in
`dependencies.json` inside the project's `Units` folder. **Unfreeze** reads the journal and puts everything
back. The order is such that no edit happens before the rollback record is written.

## Search

The field in the menu bar (see above). Searches filenames across the whole merged tree (mod + dependencies +
engine), with type filters and ranking (a filename match beats a folder match).

## Localization

* A `.ucs` string table is read from the game folder and the module folder; the language list is built from
  the files found.
* Values of the form `$id` in RGD files and the Object window resolve to the selected language's text.
* `Alt`+click on a localized string jumps to the entry in the `.ucs` (a read-only tab); `Alt+Ctrl`+click (or a
  click on an unresolved id) opens the edit/create dialog, which writes into the mod's `.ucs`.

## SGA

* `Mod → Pack SGA…` — packs the current mod's files into an SGA v2 archive and updates the `.module`.
* `Mod → Extract SGA…` — unpacks selected mod archives to disk for viewing/editing.
* Engine and parent-mod archives are read in place and shown in the tree (colors above).

## Settings (File → Settings)

* **Dark/light theme** — applied immediately; Cancel restores the previous one.
* **Graphics renderer**: `Vulkan` (default), `Direct3D 11`, `Software` — applied at the next start (through the
  `DOW_GPU_API` environment variable). The software renderer is the escape hatch from hardware-renderer bugs.
* **Game folder** — it determines the set of languages and localization tables.
* **Text editor formats** — a comma-separated list of extensions (`.lua, .txt, .mydsl`); clearing the field
  restores the built-in defaults.

## Where data lives

Everything personal sits under `%AppData%\DoW Mod Studio\`:

```
state.conf   — session state (theme, renderer, game folder, open module and tabs, language,
               expanded folders, object-window selection); saved automatically
recycle\     — the recycle bin for deleted mod files
Projects\<Mod>\Units\                  — units/buildings moved out by Freeze
Projects\<Mod>\Units\dependencies.json — the journal of references cleared by Freeze
```

## Keyboard shortcuts

| Key | Action |
|---|---|
| `↑` `↓` `←` `→` | Navigate and collapse/expand in the file tree and the RGD tree |
| `Enter` | Open the selected file / follow an RGD reference |
| `Delete` | Delete the selected mod file (with confirmation) |
| `Esc` | Close a context menu |
| Wheel on the tab bar | Scroll the tabs sideways |
| `Alt`+click / `Alt+Ctrl`+click on a localized string | Jump to the `.ucs` / edit the localization entry |

In the Events window: `Ctrl`+click selects multiple rows, `Shift`+click a range; in the bone pose bar the same
modifiers work on keyframes.

## Command line and associations

The program opens a `.module` passed as an argument (`dow-mod-studio.exe mymod.module`) — this is what a
double-click does after the association is registered. Service flags:

```
--register     / --register-assoc   register the .module association
--unregister   / --unregister-assoc remove the association
--assoc-status / --status           print the association state
```

Helper utilities in `cmd/`: `audiotest` (audio-path check) and `sgafind` (console SGA search).

## Building from sources

Go 1.27.1 is required. On Windows:

```
d_build_windows.bat
```

The script builds `dow-mod-studio.exe` (`-trimpath -ldflags "-s -w -H windowsgui"`).
A plain `go build ./...` produces a console binary and does not refresh the checked-in `.exe` — use the
script (an explicit `-o`) when a runnable binary is needed.

Check with:

```
go test ./...
go build ./...
```

> **Note for a fresh clone:** `go.mod` contains `replace` directives pointing at a local fork of Gio
> (paths like `C:/Users/…/gio`), including `vulkan-go` and a fork of the shaders. To build on your machine,
> repoint these paths at your own checkouts of the fork (or remove the `replace` directives and use upstream
> Gio — the hardware Vulkan/D3D renderer may then be unavailable).

## Repository layout

The application code lives in the root package (`main`, one file group per subsystem: `unit_*.go`,
`viewer_*.go`, `fx_*.go`, `locale_*.go`, `fda_*.go`, `rgd_*.go`, `sga_*.go`, `freeze*.go`, `graph_ui.go`, …).
The game formats themselves are encapsulated under `internal/`:

| Package | Purpose |
|---|---|
| `internal/modtree` | `.module` parsing, the merged source tree (mod / dependencies / engine), search |
| `internal/rgd` | the binary RGD format: reading, writing, types, references |
| `internal/fda` | the Relic FDA audio codec (decode, resample, WAV encoding) |
| `internal/wav`, `internal/audioplayer` | WAV and buffered playback |
| `internal/whm` | `.whm` models, skeleton, animations, `.sgm` export |
| `internal/whe` | the `.whe` effects format: events and timelines |
| `internal/fx` | effect simulation and rendering (sprites, ribbons, beams), Lua definitions, sounds |
| `internal/sga` | reading SGA archives; SGA v2 writing lives in the app's `sga_pack.go` |
| `internal/raster` | image decoders: DDS (incl. BC7), TGA, RSH, RTX, PNG/JPEG/BMP |
| `internal/3d_engine`, `internal/3d_viewer` | software and hardware renderers, camera, axes |
| `internal/chunky` | the game's chunked container |

## Acknowledgments

This project relies heavily on the work of the open-source community and other developers. Special thanks to:

* [amorgun/dow_utils](https://github.com/amorgun/dow_utils) — for the `dow_utils` library and the detailed
  breakdown of the file formats.
* The [vgmstream](https://github.com/vgmstream/vgmstream) team — for their incredible collaborative effort.
  I used their encoder and decoder for FDA audio files, as well as the implementation of Relic
  Entertainment's modified AIFF codec, which the team successfully reverse-engineered from the original
  game utilities.
* The CodebuffAI team — for their wonderful Freebuff service.
* The UA team — for the warm welcome into their DoW Ultimate Apocalypse mod development team.
* Relic Entertainment — for the amazing Dawn of War game series.
* Google — for the advice and help from Gemini :)

---

An unofficial tool, not affiliated with Relic / Sega / Games Workshop. Game and format names belong to
their owners.
