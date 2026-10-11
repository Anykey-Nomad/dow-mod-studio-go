# `.sga` Archive Format Breakdown (Relic Game Data Archive)

The `.sga` format is used by the Relic engine to pack folders of game assets loaded by the game and mod tools[cite: 11]. This parser supports archives from Dawn of War (format version 2) and Company of Heroes (format version 4)[cite: 11].

* Read this in other languages: [Русский](Readme_ru.md)

## Archive Structure

* **Magic Signature:** The file always begins with the magic string `_ARCHIVE`[cite: 11].
* **Version & Platform:** Followed by the version (2 or 4). For version 4, a platform identifier is also checked (expecting `1` for little-endian)[cite: 11].
* **Data Header:** Stores sizes and offsets for the archive's main tables[cite: 11].

The archive consists of three interconnected tables:
1. **Table of Contents (ToC):** Entry points or aliases (e.g., "Data" or "ToC0") that point to specific ranges of directories and files[cite: 11].
2. **Directories:** Contain the directory name and index ranges pointing to its subdirectories and files[cite: 11].
3. **Files:** Each entry stores the file name, its offset relative to the data block, its compressed length, and its original (uncompressed) length[cite: 11]. Versions 2 and 4 have slightly different layouts for file entries[cite: 11].

## Compression

* Files within the archive are compressed using `zlib`[cite: 11].
* If the compressed length (`compLen`) equals the original length (`length`), the file is stored uncompressed and read directly[cite: 11].

## Implementation Details (Caching & Concurrency)

* **Index Caching:** Parsing the archive's structure (reading tens of thousands of strings) is done only once. The constructed index (directory tree and file table) is cached in memory, keyed by the file path, size, and modification time[cite: 11]. This allows for instant reopening of the archive.
* **Concurrent Reading:** Inflating hundreds of megabytes of `zlib` data on a single CPU core is slow[cite: 11]. Batch reading of files (e.g., a model's textures) is implemented concurrently using a worker pool (goroutines), since `ReadAt` is thread-safe and does not move the file pointer[cite: 11].
