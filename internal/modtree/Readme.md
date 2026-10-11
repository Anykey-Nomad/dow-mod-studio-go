# `modtree` Package Breakdown (Virtual File System)

The `modtree` package parses `.module` descriptors and constructs a unified virtual file system (VFS) tree for *Dawn of War* mods. It merges disparate data from physical disk folders and `.sga` archives into a single hierarchy, strictly respecting mod dependencies and override priorities.

* Read this in other languages: [Русский](README.ru.md)

## Merging and Priorities (Virtual File Tree)

* All resources from the active mod, required mods, and the base game are mapped into a single virtual file system starting at the `Data` root.
* Source loading follows a strict priority order:
  * **Current Mod (`KindThisMod`):** Highest priority (starts at 0).
  * **Required Mods (`KindReqMod`):** Medium priority (starts at 15000).
  * **Base Engine (`KindEngine`):** Lowest priority (30001).
* In the event of file name conflicts (duplicates), the file from the source registered first (highest priority) wins. However, the contents of identically named folders are seamlessly merged together.

## Optimization and Concurrency

* The discovery and registration of sources is executed sequentially to ensure the override order remains deterministic.
* The actual file system traversal and `.sga` table parsing (`buildPending`) are executed concurrently using a worker pool (`runtime.NumCPU()`).
* This separation drastically speeds up mod loading, as walking thousands of directories and reading archive indices accounts for the vast majority of the load time.

## Indexing and Search

* The base name file index is built lazily (on first use) and is protected by a mutex to prevent race conditions during concurrent access.
* The package implements an advanced, case-insensitive multi-word search:
  * Every word in a query must appear in either the file's name or its folder path.
  * Search results are scored and ranked: matches within the file name itself are prioritized over folder-only matches (which receive a penalty of `folderScoreBias = 100`).
  * Ties are broken based on the source's priority rank (the active mod always floats to the top).
* Built-in filtering is provided via `SearchKind` (RGD, Lua, FDA, ART/textures, WHM).

## Overlay (Editing) and Parsing

* The package supports an "overlay" mode to write modifications back to the mod's files. The `WriteOverlayFile` method saves data to the correct physical directory on the disk (creating missing folders) and immediately upserts the file into the virtual merged tree.
* A custom `.module` file parser reads the INI-like syntax, extracting `DataFolder.N`, `ArchiveFile.N`, and `RequiredMod.N` sequences.
* The parser automatically resolves Corsix dynamic paths, replacing tokens like `%LOCALE%` or `%TEXTURE-LEVEL%` with their actual string values.