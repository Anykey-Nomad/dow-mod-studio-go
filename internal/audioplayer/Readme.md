# `audioplayer` Package Breakdown

The `audioplayer` package owns the application's single audio device context and manages the chunked PCM source that feeds it[cite: 22]. FDA playback, WAV previews, and the FX sound player all route their audio through this single pipeline[cite: 22].

* Read this in other languages: [Русский](Readme_ru.md)

## Device Initialization

* The audio device context is opened exactly once per process using the `oto/v3` library and a `sync.Once` primitive[cite: 22].
* Every sound is played using the exact format that the context was initially opened with[cite: 22].

## Fixed Audio Format

* The playback format is strictly hardcoded to a 48000 Hz sample rate and 2 channels (stereo)[cite: 22].
* In-game sounds are natively mono and typically authored at either 22050 or 44100 Hz[cite: 22].
* **Reason for hardcoding:** The `oto` documentation notes that Windows WASAPI reliably supports 48000 Hz stereo, but mono streams or other sample rates are unlikely to be supported natively[cite: 22].
* If left to dynamically select the format based on the first sound played, the system falls back to the older WinMM API, allowing the device to choose its own rate[cite: 22].
* This WinMM fallback causes overlapping sounds to become distorted, stretched, and pitched down[cite: 22].
* To prevent this, all decoding paths must up-sample and convert every sound into 48000 Hz stereo *before* it reaches the player[cite: 22].

## Streaming and Seeking (Chunking)

* The raw PCM stream is split into fixed-size chunks of 50 milliseconds via the `ChunkBuffer` structure[cite: 22].
* The `ChunkSource` struct implements the `io.Reader` and `io.Seeker` interfaces to consume these chunks[cite: 22].
* `ChunkSource` is thread-safe, utilizing a `sync.Mutex` lock, which allows another goroutine (like a UI seek bar) to safely poll the current byte position while the audio is actively playing[cite: 22].
* Seeking operations are intentionally snapped to chunk boundaries, which keeps seeks computationally cheap and ensures the stream remains perfectly aligned[cite: 22].
