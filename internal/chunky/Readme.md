# `Relic Chunky` Container Format Breakdown

`Relic Chunky` is a universal hierarchical container format shared across multiple Relic engine files, including 3D models (`.whm`, `.sgm`), event files (`.whe`), textures (`.rsh`), and FX files[cite: 21].

* Read this in other languages: [Русский](README_ru.md)

## Container Structure

* **Preamble:** Every Chunky file begins with a fixed 24-byte preamble containing the magic string `Relic Chunky` followed by three `int32` values (1706509, 1, 1)[cite: 21].
* **Chunk Tree:** The internal data is organized as a tree of nodes, or "chunks"[cite: 21].
  * Chunks with a `TypeID` starting with `FOLD` act as folders and contain child chunks[cite: 21]. Folder recursion is strictly capped at a depth of 16[cite: 21].
  * Chunks starting with `DATA` hold format-specific, opaque binary payloads[cite: 21].

## Chunk Header

Every chunk begins with a 20-byte header, followed by a variable-length name and its payload[cite: 21]. The header layout is:
* **`TypeID`**: 8 bytes (ASCII string, e.g., `FOLDMSGR` or `DATADATA`)[cite: 21].
* **`Version`**: 4 bytes (`int32`), representing the version of the payload data[cite: 21].
* **`Size`**: 4 bytes (`int32`), representing the size of the payload (which includes all children for folder chunks)[cite: 21].
* **`NameLen`**: 4 bytes (`int32`), defining the length of the chunk's name[cite: 21].
* **`Name`**: The chunk's name of length `NameLen`, normally terminated by a NUL byte (`\x00`)[cite: 21].

## Parsing and Serialization

* The parser utilizes a bounds-checked `Reader` cursor to safely consume little-endian primitives and length-prefixed strings[cite: 21].
* The reader is designed to fail fast on corrupted files by validating array sizes and string lengths before allocation[cite: 21].
* In addition to parsing the entire chunk tree into memory, the package can scan for file `Spans`[cite: 21]. This returns the exact byte offsets of chunk headers and boundaries, enabling in-place editing (splicing) without deep-parsing or fully rebuilding the container[cite: 21].