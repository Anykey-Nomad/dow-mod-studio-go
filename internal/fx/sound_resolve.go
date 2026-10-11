package fx

import (
	"bytes"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/wav"
	"dow-mod-studio/internal/whe"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"dow-mod-studio/internal/modtree"
)

// A sound path in a .whe is resolved against the mod tree the same way a
// texture is: the tree first, so that a mod's own sounds win over the base
// game's, and then the folders on disk the tree was built from. A path that
// names a folder of variants comes back as the whole folder, because which
// sample plays is the game's decision at the moment it plays and the preview
// has to be able to make the same one again.

// SoundBundle is one sound as it will be played: the files that may serve it
// and the one picked to start with. Variants is empty for a sound that names a
// single file.
type SoundBundle struct {
	// Path is the path as the .whe wrote it, which is what the log names.
	Path string
	// Files are the samples that may serve the sound, in a stable order.
	Files []string
}

// ResolveSound finds the files a sound path names. It reports false when
// nothing under the path exists, which is a normal thing for a mod's own .whe
// to say: a sound path can name a sample the mod does not ship, and the base
// game has one of its own under a different name.
func ResolveSound(tree *modtree.ModTree, roots []string, p string) (SoundBundle, bool) {
	b := SoundBundle{Path: p}
	var any bool
	for _, resolved := range whe.SoundPathResolved(p) {
		for _, cand := range whe.SoundVariantCandidates(resolved) {
			files, ok := fxSoundFilesAt(tree, roots, cand)
			if !ok {
				continue
			}
			for _, f := range files {
				if !fxSoundHas(b.Files, f) {
					b.Files = append(b.Files, f)
				}
			}
			any = true
		}
	}
	if !any || len(b.Files) == 0 {
		return SoundBundle{}, false
	}
	sort.Strings(b.Files)
	return b, true
}

// fxSoundFilesAt lists what one resolved path names: a single file, or every
// sample in a folder of variants. The folder case is the one the game's own
// sounds use, and it is why a sound can be heard to differ each time.
func fxSoundFilesAt(tree *modtree.ModTree, roots []string, p string) ([]string, bool) {
	p = whe.SoundPathCleans(p)
	if p == "" {
		return nil, false
	}
	// A path that names a file is that file, whichever way it was spelled.
	for _, cand := range []string{p, p + ".fda", strings.ToLower(p) + ".fda"} {
		if fxSoundFileExists(tree, roots, cand) {
			return []string{cand}, true
		}
	}
	// Otherwise a folder of variants: the directory named by the path, with a
	// trailing extension removed, which is how a .con marker and its folder sit
	// side by side.
	dir := p
	if ext := whe.LastExt(dir); ext != "" && len(ext) <= 5 {
		dir = dir[:len(dir)-len(ext)-1]
	}
	var out []string
	seen := map[string]bool{}
	addDir := func(dirPath string) {
		for _, name := range fxSoundDirEntries(tree, roots, dirPath) {
			if !strings.EqualFold(whe.LastExt(name), "fda") || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	addDir(dir)
	addDir(p)
	if len(out) == 0 {
		return nil, false
	}
	return out, true
}

// fxSoundHas reports whether a list already names a sample, which is what keeps
// the same variant from being added twice when a path resolves through more than
// one spelling of itself.
func fxSoundHas(xs []string, s string) bool {
	for _, x := range xs {
		if x == s {
			return true
		}
	}
	return false
}

// fxSoundFileExists reports whether a virtual path names a readable file, in
// the tree or on the folders it was built from.
//
// The answer comes from the table of contents rather than from reading the file
// out of the archive: resolving one sound asks this of several spellings of its
// name, and every one of those used to cost a full read and inflate of a .fda
// the caller was only asking whether it existed. A name that is listed but will
// not decode is now found here and fails later, where it is read for real.
func fxSoundFileExists(tree *modtree.ModTree, roots []string, p string) bool {
	if tree != nil {
		if tree.HasFile(p) {
			return true
		}
		for _, name := range []string{filepath.Base(p), strings.ToLower(filepath.Base(p))} {
			if f := tree.FindByName(name); f != nil && tree.HasTreeFile(f) {
				return true
			}
		}
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, name := range []string{p, strings.ToLower(p)} {
			full := filepath.Join(root, filepath.FromSlash(name))
			st, err := os.Stat(full)
			if err == nil && st.Mode().IsRegular() && st.Size() > 0 {
				return true
			}
		}
	}
	return false
}

// fxSoundDirEntries lists the .fda files inside a virtual folder, in the tree
// first and then on the folders it was built from. The tree is walked rather
// than listed because the tree has no directory listing that covers a folder of
// variants any better than the disk does, and because the disk folders are
// where the shipped sounds actually are.
func fxSoundDirEntries(tree *modtree.ModTree, roots []string, dir string) []string {
	dir = whe.SoundPathCleans(dir)
	if dir == "" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	// The tree, by the folder's own listing. A folder of variants is a folder
	// in the tree like any other, and the tree knows its contents even when
	// the disk folder it came from has been repacked into an SGA.
	if tree != nil {
		if folder := tree.FindFolder(dir); folder != nil {
			for _, f := range folder.Files {
				name := f.FullPath
				if seen[strings.ToLower(name)] {
					continue
				}
				seen[strings.ToLower(name)] = true
				out = append(out, name)
			}
		}
	}
	// The folders the tree was built from, which is where the game's own
	// sounds live and where a mod's own sounds are read from first.
	for _, root := range roots {
		if root == "" {
			continue
		}
		ents, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			continue
		}
		for _, e := range ents {
			if e.IsDir() {
				continue
			}
			name := dir + `\` + e.Name()
			if seen[strings.ToLower(name)] {
				continue
			}
			seen[strings.ToLower(name)] = true
			out = append(out, name)
		}
	}
	return out
}

// SoundRead decodes one sample's bytes into PCM ready for playback.
func SoundRead(tree *modtree.ModTree, roots []string, p string) (*wav.Info, error) {
	data, err := fxSoundBytes(tree, roots, p)
	if err != nil {
		return nil, err
	}
	return wav.Parse(data)
}

// fxSoundBytes reads a sample and returns it as a PCM WAV: an .fda is decoded
// and re-wrapped, a .wav is passed through. The extension on disk is what
// decides, because the two share no header and an .fda read as PCM is noise.
func fxSoundBytes(tree *modtree.ModTree, roots []string, p string) ([]byte, error) {
	p = whe.SoundPathCleans(p)
	if !strings.EqualFold(whe.LastExt(p), "fda") {
		return fxSoundRaw(tree, roots, p)
	}
	data, err := fxSoundRaw(tree, roots, p)
	if err != nil {
		return nil, err
	}
	parsed, err := fda.ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", p, err)
	}
	pcm, err := fda.DecodeFDAToPCM(parsed)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", p, err)
	}
	info := &fda.INFOChunk{
		Channels:     uint32(parsed.Info.Channels),
		SampleSize:   16,
		BlockBitrate: uint32(parsed.Info.BlockBitrate),
		SampleRate:   uint32(parsed.Info.SampleRate),
	}
	var buf bytes.Buffer
	if err := fda.WriteWAVTo(&buf, info, fda.PCM16Bytes(pcm)); err != nil {
		return nil, fmt.Errorf("wrap %s: %w", p, err)
	}
	return buf.Bytes(), nil
}

func fxSoundRaw(tree *modtree.ModTree, roots []string, p string) ([]byte, error) {
	if tree != nil {
		if data, err := tree.ReadFile(p); err == nil && len(data) > 0 {
			return data, nil
		}
		base := filepath.Base(p)
		for _, name := range []string{base, strings.ToLower(base), strings.TrimSuffix(base, filepath.Ext(base))} {
			if name == "" {
				continue
			}
			if f := tree.FindByName(name); f != nil {
				if data, err := tree.ReadTreeFile(f); err == nil && len(data) > 0 {
					return data, nil
				}
			}
		}
	}
	for _, root := range roots {
		if root == "" {
			continue
		}
		for _, name := range []string{p, strings.ToLower(p)} {
			if data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name))); err == nil && len(data) > 0 {
				return data, nil
			}
		}
	}
	return nil, whe.SoundMissing(p)
}
