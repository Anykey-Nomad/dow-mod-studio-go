# `.whe` File Format Breakdown (Relic Chunky)

The `.whe` files are used in the Relic engine to store visual effects (FX), sounds, and link them to unit animations. The format is based on the hierarchical Relic Chunky file structure.

* Read this in other languages: [Русский](Readme_ru.md)

## How it Works
A unit's effects are wired up in three steps:
1. **Blueprint:** Entity blueprints (`attrib/ebps/**.lua`) map gameplay triggers onto named events.
2. **Events File:** The event name resolves to an `.events` file located under `art/events`.
3. **Marker Binding:** The `.events` file specifies which `.lua` effect to play and where (usually on a marker declared in the model's `DATAMARK` chunk).

## Main Components (Chunks)

### 1. `DATAEVNT` (Event Definitions)
* Located in the `FOLDEVCT` folder.
* The chunk's name is the event name.
* Stores key/value pairs (e.g., `fx` path, marker bindings, selectors like `random_set`).
* **Sound Events:** Identified by the `sound_` prefix. They contain a path to a sound directory (relative to `data/sound`) instead of an FX path. An empty payload (4 zero bytes) indicates a silent event. Sound paths support the `%terrain%` token, which dynamically resolves to dirt, sand, grass, rock, or water.

### 2. `DATAMTON` (Animation Timelines)
* Located in the `FOLDMTRE` folder.
* Defines which events fire during an animation, based on seconds into the clip.
* Special timing values:
  * `-1`: Fires on entering the animation.
  * `MAX_FLOAT`: Fires on leaving the animation.
* The end of the chunk contains a "Motion Tail" that dictates playback behavior: looping, start/exit delays, transitions, and modifiers.

### 3. `DATAACTS` (Action Layer)
* Sits above the motion timelines. It contains named actions that decide which motions to play and which sub-actions to run.
* Uses an `if / else if / else / always` conditional chain to guard each choice. These compare types are represented internally by the integers `0`, `1`, `2`, and `3`.
