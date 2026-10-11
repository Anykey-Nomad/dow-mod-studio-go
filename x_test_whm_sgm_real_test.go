package main

import (
	"bytes"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/whm"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Optional integration check against a real Dawn of War install. It is skipped
// when the game is not present, so the normal test run stays hermetic.
const (
	dowInstallDir = `C:\Program Files\Steam\steamapps\common\Dawn of War Definitive Edition\UltimateApocalypse_THB_test\data`
	dowRacesDir   = dowInstallDir + `\art\ebps\races`
	dowTroopsDir  = dowRacesDir + `\Necrons\troops`
)

func TestConvertRealDowModels(t *testing.T) {
	entries, err := os.ReadDir(dowRacesDir)
	if err != nil {
		t.Skipf("no Dawn of War install at %s", dowRacesDir)
	}
	converted := 0
	total := 0
	var unsupported []string
	for _, race := range entries {
		if !race.IsDir() {
			continue
		}
		for _, sub := range []string{"troops", "vehicles"} {
			dir := filepath.Join(dowRacesDir, race.Name(), sub)
			files, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, e := range files {
				if e.IsDir() || filepath.Ext(e.Name()) != ".whm" {
					continue
				}
				total++
				path := filepath.Join(dir, e.Name())
				raw, err := os.ReadFile(path)
				if err != nil {
					t.Errorf("%s: %v", path, err)
					continue
				}
				label := race.Name() + "/" + e.Name()
				sgm, info, err := whm.ConvertToSgm(raw)
				if err != nil {
					// A handful of shipped meshes use a per-vertex block that
					// does not match the documented stride. Those must fail
					// loudly instead of producing a corrupt .sgm, so they are
					// reported rather than treated as a test failure.
					unsupported = append(unsupported, fmt.Sprintf("%s: %v", label, err))
					continue
				}
				verifyConvertedModel(t, label, raw, sgm, info)
				verifyGeometryPreserved(t, label, raw, sgm)
				converted++
			}
		}
	}
	for _, u := range unsupported {
		t.Logf("unsupported: %s", u)
	}
	t.Logf("converted %d of %d models", converted, total)
	if total > 0 && converted*10 < total*9 {
		t.Errorf("only %d of %d models converted, coverage regressed", converted, total)
	}
}

// verifyConvertedModel re-parses the produced .sgm and checks every invariant
// the Object Editor format depends on.
func verifyConvertedModel(t *testing.T, name string, raw, sgm []byte, info *whm.ModelInfo) {
	t.Helper()
	chunks, consumed := chunky.Parse(sgm[chunky.PreambleSize:], 0)
	if consumed != len(sgm)-chunky.PreambleSize {
		t.Errorf("%s: %d trailing bytes after the root chunk", name,
			len(sgm)-chunky.PreambleSize-consumed)
	}
	if len(chunks) != 1 || chunks[0].TypeID != "FOLDRSGM" {
		t.Fatalf("%s: unexpected root chunks", name)
	}
	root := chunks[0]
	if root.Version != 1 {
		t.Errorf("%s: FOLDRSGM version %d", name, root.Version)
	}
	if !bytes.Equal(chunky.Serialize(root), sgm[chunky.PreambleSize:]) {
		t.Errorf("%s: re-serialization is not byte stable", name)
	}

	bones := 0
	meshes := 0
	anims := 0
	animTracks := 0
	for _, c := range root.Children {
		switch c.TypeID {
		case "FOLDSKEL":
			bones = verifySkel(t, name, c)
		case "FOLDMSGR":
			meshes = verifyMeshes(t, name, c)
		case "FOLDANIM":
			anims++
			animTracks += len(c.Children)
		case "DATAMARK", "DATACMRA":
			if c.Name == "" {
				t.Errorf("%s: %s chunk has an empty name", name, c.TypeID)
			}
		case "DATAFBIF", "DATASKEL", "DATACAMS", "DATAANBV", "DATADATA":
			t.Errorf("%s: .whm only chunk %s survived", name, c.TypeID)
		}
	}
	if bones != info.Bones {
		t.Errorf("%s: bones %d != %d", name, bones, info.Bones)
	}
	if meshes != info.Meshes {
		t.Errorf("%s: meshes %d != %d", name, meshes, info.Meshes)
	}
	if anims != info.Animations {
		t.Errorf("%s: animations %d != %d", name, anims, info.Animations)
	}
	t.Logf("%-34s bones=%-4d meshes=%-3d verts=%-7d anims=%-3d tracks=%-4d markers=%-3d cams=%-2d tex=%-2d  %d -> %d bytes",
		name, info.Bones, info.Meshes, info.Vertices, info.Animations, animTracks,
		info.Markers, info.Cameras, info.Textures, len(raw), len(sgm))
}

func verifySkel(t *testing.T, name string, skel *chunky.Chunk) int {
	t.Helper()
	if skel.Version != 3 {
		t.Errorf("%s: FOLDSKEL version %d", name, skel.Version)
	}
	info := skel.Child("DATAINFO")
	if info == nil || info.Version != 1 || len(info.Payload) != 4 {
		t.Fatalf("%s: FOLDSKEL/DATAINFO = %+v", name, info)
	}
	count := int(binary.LittleEndian.Uint32(info.Payload))
	boneChunks := 0
	for _, b := range skel.Children {
		if b.TypeID != "DATABONE" {
			continue
		}
		boneChunks++
		if b.Version != 5 {
			t.Errorf("%s: DATABONE version %d", name, b.Version)
		}
		if len(b.Payload) != 32 {
			t.Errorf("%s: bone %q payload %d bytes", name, b.Name, len(b.Payload))
		}
		// parent index must reference an already emitted bone or -1
		parent := int32(binary.LittleEndian.Uint32(b.Payload))
		if parent >= int32(boneChunks-1) && parent != -1 {
			t.Errorf("%s: bone %q parent %d is not a previous bone", name, b.Name, parent)
		}
	}
	if count != boneChunks {
		t.Errorf("%s: DATAINFO says %d bones, %d emitted", name, count, boneChunks)
	}
	return count
}

func verifyMeshes(t *testing.T, name string, msgr *chunky.Chunk) int {
	t.Helper()
	meshes := 0
	for _, m := range msgr.Children {
		if m.TypeID != "FOLDMSLC" {
			continue
		}
		meshes++
		if m.Child("DATADATA") == nil {
			t.Errorf("%s: mesh %q has no DATADATA", name, m.Name)
		}
		if m.Child("DATABVOL") == nil {
			t.Errorf("%s: mesh %q lost its bounding volume", name, m.Name)
		}
	}
	return meshes
}

// verifyGeometryPreserved walks the .whm mesh layout independently and compares
// it with the .sgm, proving the geometry really survived the rewrite rather
// than merely parsing without error.
func verifyGeometryPreserved(t *testing.T, name string, raw, sgm []byte) {
	t.Helper()
	inRoot := firstRoot(t, raw)
	outRoot := firstRoot(t, sgm)
	inMeshes, inVerts, inMats, inVertexBytes := walkMeshData(t, name, inRoot, true)
	outMeshes, outVerts, outMats, outVertexBytes := walkMeshData(t, name, outRoot, false)
	if inMeshes != outMeshes || inVerts != outVerts {
		t.Errorf("%s: geometry changed: %d/%d verts -> %d/%d verts",
			name, inVerts, inMeshes, outVerts, outMeshes)
	}
	if inMats != outMats {
		t.Errorf("%s: material references changed: %d -> %d", name, inMats, outMats)
	}
	if inVertexBytes != outVertexBytes {
		t.Errorf("%s: vertex data is %d bytes, want %d", name, outVertexBytes, inVertexBytes)
	}
}

func firstRoot(t *testing.T, blob []byte) *chunky.Chunk {
	t.Helper()
	chunks, _ := chunky.Parse(blob[chunky.PreambleSize:], 0)
	for _, c := range chunks {
		if c.TypeID == "FOLDRSGM" {
			return c
		}
	}
	t.Fatal("no FOLDRSGM")
	return nil
}

// walkMeshData returns meshes, vertices, material references and the total byte
// size of the vertex arrays. whm selects the .whm record layout, which carries
// the reserved dwords and the trailing shadow volume.
func walkMeshData(t *testing.T, name string, root *chunky.Chunk, isWhm bool) (int, int, int, int) {
	t.Helper()
	msgr := root.Child("FOLDMSGR")
	if msgr == nil {
		return 0, 0, 0, 0
	}
	meshes, verts, mats, vertexBytes := 0, 0, 0, 0
	for _, m := range msgr.Children {
		if m.TypeID != "FOLDMSLC" {
			continue
		}
		d := m.Child("DATADATA")
		if d == nil {
			t.Errorf("%s: mesh %q has no DATADATA", name, m.Name)
			continue
		}
		meshes++
		p := d.Payload
		r := &chunky.Reader{Data: p}
		r.Take(13)
		skin := r.Count(4)
		for i := 0; i < skin; i++ {
			r.RawStr()
			r.Skip(4)
		}
		count := r.Count(4)
		sizeID := r.I32()
		if r.Err != nil {
			t.Errorf("%s: mesh %q header: %v", name, m.Name, r.Err)
			continue
		}
		stride := 32
		if sizeID > 37 {
			stride = 48
		}
		vertexStart := r.Off
		matStart, strip := -1, 0
		for _, cand := range []struct{ off, strip int }{
			{vertexStart + count*stride + 4, 4},
			{vertexStart + count*stride, 0},
		} {
			if whm.MeshMaterialSection(p, cand.off, isWhm, false) > 0 {
				matStart, strip = cand.off, cand.strip
				break
			}
		}
		if matStart < 0 {
			for off := vertexStart + 4; off+8 < len(p); off += 2 {
				if whm.MeshMaterialSection(p, off, isWhm, true) > 0 {
					matStart = off
					if off-4 >= vertexStart && chunky.AllZero(p[off-4:off]) {
						strip = 4
					}
					break
				}
			}
		}
		if matStart < 0 {
			t.Errorf("%s: mesh %q: material section not found", name, m.Name)
			continue
		}
		verts += count
		vertexBytes += matStart - strip - vertexStart
		matEnd := whm.MeshMaterialSection(p, matStart, isWhm, false)
		mats += int(int32(binary.LittleEndian.Uint32(p[matStart:])))
		if !isWhm && matEnd != len(p) {
			t.Errorf("%s: mesh %q payload has %d trailing bytes, want none", name, m.Name, len(p)-matEnd)
		}
	}
	return meshes, verts, mats, vertexBytes
}

func TestDumpModelTree(t *testing.T) {
	if os.Getenv("DOW_DUMP") == "" {
		t.Skip("set DOW_DUMP to a .whm path")
	}
	raw, err := os.ReadFile(os.Getenv("DOW_DUMP"))
	if err != nil {
		t.Fatal(err)
	}
	chunks, _ := chunky.Parse(raw[chunky.PreambleSize:], 0)
	var dump func(prefix string, c *chunky.Chunk)
	dump = func(prefix string, c *chunky.Chunk) {
		fmt.Printf("%s%-9s v%-2d %8d  %q\n", prefix, c.TypeID, c.Version, len(c.Payload), c.Name)
		for _, ch := range c.Children {
			dump(prefix+"  ", ch)
		}
	}
	for _, c := range chunks {
		dump("", c)
	}
}
