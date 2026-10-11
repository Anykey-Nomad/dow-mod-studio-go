# `.fda` Audio Format Breakdown (Relic Codec)

The `.fda` files are used for compressed audio storage and are built on top of the Relic Chunky container format. The audio is encoded using a proprietary Discrete Cosine Transform (DCT) based codec.

* Read this in other languages: [Русский](README.ru.md)

## File Structure (Chunks)

* The `.fda` file format is based on the `Relic Chunky` hierarchical structure and utilizes a mono-interleaved, DCT-based audio codec[cite: 7, 10].
* The file header always contains the `Relic Chunky` signature[cite: 10].
* The file consists of several main chunks: `FBIF` (plugin and creation time info), `FDA ` (main container), `INFO` (audio metadata like channels, sample rate, bitrate, and loop points), and `DATA` (the raw compressed audio)[cite: 10].

## Codec Details (Relic Codec)

* The codec processes audio in frames, with a fixed frame size of 512 samples[cite: 7].
* Data is stored per-channel in a sequential manner, such as `[ch0_frame][ch1_frame][ch0_frame][ch1_frame]`[cite: 8].
* Compression involves dividing the frequency spectrum into 27 critical bands[cite: 7, 9].
* The codec relies on a mixed-radix Fast Fourier Transform (MixFFT) algorithm to perform transformations between the time and frequency domains (DCT/IDCT)[cite: 6, 7, 9].

## The Importance of Sample Rate

* The frequency table size used by the codec is strictly tied to the sample rate: 128 for rates below 22050 Hz, 256 for exactly 22050 Hz, and 512 for higher rates[cite: 7, 8].
* Decoding with an incorrect sample rate causes the codec to unpack using the wrong table, resulting in irreversible audio corruption that cannot be fixed by resampling[cite: 8].