package main

import (
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/modtree"
	"encoding/binary"
	"testing"

	"gioui.org/layout"
)

func TestMaterialTreePathIncludesArchiveAndVirtualPath(t *testing.T) {
	file := &modtree.TreeFile{
		FullPath: `Data\textures\slag_deposit.rtx`,
		Source: &modtree.FileSource{
			IsSGA:       true,
			ArchivePath: "/game/W40kData-SharedTextures.sga",
		},
	}
	if got, want := materialTreePath(file), "/game/W40kData-SharedTextures.sga :: Data\\textures\\slag_deposit.rtx"; got != want {
		t.Fatalf("materialTreePath = %q, want %q", got, want)
	}
}

func TestDecodeModelMaterialsReportsRTXFormat(t *testing.T) {
	got := decodeModelMaterials(map[string][]byte{"slag_deposit": diagnosticTestRTX(t, 8)}, true)
	diag := got.diagnostics["slag_deposit"]
	if diag.Format != "RTX / DXT1 (BC1)" || diag.Status != "decoded" {
		t.Fatalf("format/status = %q/%q, want RTX / DXT1 (BC1)/decoded", diag.Format, diag.Status)
	}
	if got.materials["slag_deposit"].Texture == nil {
		t.Fatal("diagnostic decode did not produce a usable material image")
	}
}

func TestModelMaterialDiagnosticsTableHandlesMissingViewer(t *testing.T) {
	ui := &uiState{}
	for _, tab := range []*fileTab{nil, {}} {
		if got := ui.modelMaterialDiagnosticsTable(layout.Context{}, tab); got != (layout.Dimensions{}) {
			t.Errorf("material diagnostics for tab %v = %+v, want empty dimensions", tab, got)
		}
	}
}

func diagnosticTestRTX(t *testing.T, format uint32) []byte {
	t.Helper()
	attr := make([]byte, 16)
	binary.LittleEndian.PutUint32(attr[0:4], format)
	binary.LittleEndian.PutUint32(attr[4:8], 4)
	binary.LittleEndian.PutUint32(attr[8:12], 4)
	binary.LittleEndian.PutUint32(attr[12:16], 1)
	pixels := []byte{0, 0xf8, 0, 0, 0, 0, 0, 0}
	root := &chunky.Chunk{TypeID: "FOLDRTXTR", Children: []*chunky.Chunk{
		{TypeID: "FOLDIMAG", Children: []*chunky.Chunk{
			{TypeID: "DATAATTR", Payload: attr},
			{TypeID: "DATADATA", Payload: pixels},
		}},
	}}
	return append(chunky.Preamble(), chunky.Serialize(root)...)
}
