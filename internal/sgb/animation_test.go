package sgb

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
)

func TestResolveLinkedIdleAnimationFollowsChain(t *testing.T) {
	root := t.TempDir()
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	files := map[string][]byte{
		"base.whm":   testAnimationModel("local_idle"),
		"base.whe":   testIdleWHE("local_idle", "art/ebps/shared", "shared_idle"),
		"shared.whm": testAnimationModel("shared_idle"),
		"shared.whe": testIdleWHE("shared_idle", "art/ebps/final", "final_idle"),
		"final.whm":  testAnimationModel("final_idle"),
		"final.whe":  testIdleWHE("final_idle", "", ""),
	}
	for name, data := range files {
		full := filepath.Join(root, name)
		if err := os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
		addAnimationTreeFile(tree, filepath.ToSlash(filepath.Join("Data", "art", "ebps", name)), full)
	}

	baseBytes, err := tree.ReadFile("Data/art/ebps/base.whm")
	if err != nil {
		t.Fatal(err)
	}
	base, err := whm.Parse(baseBytes)
	if err != nil {
		t.Fatal(err)
	}
	animation, source := ResolveLinkedIdleAnimation(tree, "Data/art/ebps/base.whm", base)
	if source == nil || source.Name != "test-model" {
		t.Fatalf("resolved source model = %#v, want final linked model", source)
	}
	if animation == nil || animation.Name != "final_idle" {
		t.Fatalf("resolved animation = %#v, want final_idle", animation)
	}
	if &source.Animations[0] != animation {
		t.Fatal("resolved animation does not belong to the returned source model")
	}
}

func TestResolveLinkedAnimationSelectsNamedClipWithoutUsingIdleFallback(t *testing.T) {
	root := t.TempDir()
	tree := &modtree.ModTree{Root: modtree.NewFolder("Data", "Data", nil)}
	for name, data := range map[string][]byte{
		"model.whm":  testAnimationModel("idle_1", "uncaptured_idle", "ol_uncaptured_idle"),
		"model.whe":  testIdleWHE("idle_1", "", ""),
		"linked.whm": testAnimationModel("linked_clip"),
		"linked.whe": testIdleWHE("linked_clip", "", ""),
	} {
		full := filepath.Join(root, name)
		if err := os.WriteFile(full, data, 0o600); err != nil {
			t.Fatal(err)
		}
		addAnimationTreeFile(tree, filepath.ToSlash(filepath.Join("Data", "art", "ebps", name)), full)
	}
	modelBytes, err := tree.ReadFile("Data/art/ebps/model.whm")
	if err != nil {
		t.Fatal(err)
	}
	model, err := whm.Parse(modelBytes)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"uncaptured_idle", "ol_uncaptured_idle"} {
		animation, source := ResolveLinkedAnimation(tree, "Data/art/ebps/model.whm", model, want)
		if animation == nil || animation.Name != want || source != model {
			t.Errorf("ResolveLinkedAnimation(%q) = (%v, %v), want named clip on model", want, animation, source)
		}
	}
	if animation, _ := ResolveLinkedAnimation(tree, "Data/art/ebps/model.whm", model, "missing"); animation != nil {
		t.Fatalf("missing named clip fell back to %q", animation.Name)
	}
}

func TestBindAnimationToModelMapsTracksByBoneName(t *testing.T) {
	model := &whm.Model{Bones: []whm.Bone{{Name: "Root"}, {Name: "Arm"}}}
	animation := &whm.Animation{Bones: []whm.Track{
		{Name: "arm", Bone: 17},
		{Name: "missing", Bone: 0},
		{Name: "", Bone: 1},
	}}
	bound := BindAnimationToModel(model, animation)
	if bound == animation {
		t.Fatal("binding must not mutate or return the linked model's animation object")
	}
	if got, want := []int{bound.Bones[0].Bone, bound.Bones[1].Bone, bound.Bones[2].Bone}, []int{1, -1, 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("bone indices = %v, want %v", got, want)
	}
	if animation.Bones[0].Bone != 17 {
		t.Fatalf("source clip mutated while binding: bone index = %d", animation.Bones[0].Bone)
	}
}

func addAnimationTreeFile(tree *modtree.ModTree, virtualPath, diskPath string) {
	parts := bytes.Split([]byte(virtualPath), []byte{'/'})
	folder := tree.Root
	for _, part := range parts[1 : len(parts)-1] {
		folder = tree.GetOrCreateFolder(folder, string(part), nil)
	}
	name := string(parts[len(parts)-1])
	folder.Files = append(folder.Files, &modtree.TreeFile{
		Name: name, FullPath: virtualPath, DiskPath: diskPath,
		Source: &modtree.FileSource{DiskRoot: filepath.Dir(diskPath)},
	})
	folder.FileByLower[name] = folder.Files[len(folder.Files)-1]
}

func testAnimationModel(animationNames ...string) []byte {
	var payload bytes.Buffer
	for _, value := range []int32{1} {
		_ = binary.Write(&payload, binary.LittleEndian, value)
	}
	_ = binary.Write(&payload, binary.LittleEndian, float32(1))
	_ = binary.Write(&payload, binary.LittleEndian, int32(0)) // bone tracks
	_ = binary.Write(&payload, binary.LittleEndian, int32(0)) // mesh/material tracks
	children := make([]*chunky.Chunk, 0, len(animationNames))
	for _, animationName := range animationNames {
		children = append(children, &chunky.Chunk{TypeID: "FOLDANIM", Name: animationName, Children: []*chunky.Chunk{
			{TypeID: "DATADATA", Payload: payload.Bytes()},
		}})
	}
	model := &chunky.Chunk{TypeID: "FOLDRSGM", Name: "test-model", Children: children}
	return append(chunky.Preamble(), chunky.Serialize(model)...)
}

func testIdleWHE(animationName, linkedModel, linkedAnimation string) []byte {
	motion := append(chunky.I32(1), testChunkyString(animationName)...)
	motion = append(motion, chunky.I32(0)...)
	motion = append(motion, 0) // random-pick loop flag
	motion = append(motion, chunky.I32(0)...)
	rebp := &chunky.Chunk{TypeID: "FOLDREBP", Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Payload: whe.MarshalActs([]whe.FxAction{{Name: "default", Motions: []whe.FxActionLink{{Name: "idle", Compare: whe.FxCompareNone}}}})},
		{TypeID: "FOLDMTRE", Children: []*chunky.Chunk{{TypeID: "DATAMTON", Name: "idle", Payload: motion}}},
	}}
	if linkedModel != "" {
		var xref bytes.Buffer
		writeChunkyString(&xref, linkedModel)
		writeChunkyString(&xref, linkedAnimation)
		rebp.Children = append(rebp.Children, &chunky.Chunk{TypeID: "FOLDANIM", Name: animationName, Children: []*chunky.Chunk{
			{TypeID: "DATA", Name: "XREF", Payload: xref.Bytes()},
		}})
	}
	return append(chunky.Preamble(), chunky.Serialize(rebp)...)
}

func testChunkyString(value string) []byte {
	var b bytes.Buffer
	writeChunkyString(&b, value)
	return b.Bytes()
}

func writeChunkyString(b *bytes.Buffer, value string) {
	_ = binary.Write(b, binary.LittleEndian, int32(len(value)))
	b.WriteString(value)
}
