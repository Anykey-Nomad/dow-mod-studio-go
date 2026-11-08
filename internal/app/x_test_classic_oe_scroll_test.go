package app

import (
	"bytes"
	"dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/chunky"
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/whe"
	"fmt"
	"testing"

	"gioui.org/op"
)

// classicOEConditionWhe is a .whe carrying only a FOLDCONL folder of n named
// conditions, each pointing at two clauses — enough rows that the object
// editor's column has something to scroll without the game installed.
func classicOEConditionWhe(n int) []byte {
	folder := &chunky.Chunk{TypeID: "FOLDCONL", Version: 1}
	for i := 0; i < n; i++ {
		var payload bytes.Buffer
		classicOEWheI32(&payload, 2)
		for _, name := range []string{"clause_a", "clause_b"} {
			classicOEWheI32(&payload, int32(len(name)))
			payload.WriteString(name)
		}
		folder.Children = append(folder.Children, &chunky.Chunk{
			TypeID: "DATACOND", Version: 1,
			Name:    fmt.Sprintf("cond_%02d", i),
			Payload: payload.Bytes(),
		})
	}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{folder}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	return buf.Bytes()
}

func classicOEWheI32(b *bytes.Buffer, v int32) {
	b.WriteByte(byte(v))
	b.WriteByte(byte(v >> 8))
	b.WriteByte(byte(v >> 16))
	b.WriteByte(byte(v >> 24))
}

// TestTheClassicOEColumnKeepsScrollingWithTheConditionListOpen covers the
// column losing its scrollbar and its wheel the moment the condition panel was
// opened. The panel's four buttons are wider than the 200dp column, a
// horizontal Flex whose children overflow reports the height it was handed
// back as its own, and PanelScrollView then reads the column as elastic
// content: it measures it at the viewport instead, concludes the content fits,
// and turns the scrollbar and the wheel off.
//
// The list of conditions itself was never the problem — the button row of the
// clause editor underneath it was, which is why the bar vanished on selecting
// a condition as much as on opening the panel over a file that has them.
func TestTheClassicOEColumnKeepsScrollingWithTheConditionListOpen(t *testing.T) {
	ui := newUI(testutil.UnitTestTheme())
	doc, err := whe.ParseDocument(classicOEConditionWhe(60))
	if err != nil {
		t.Fatalf("whe.ParseDocument: %v", err)
	}
	ui.UnitEvtDoc = doc

	// Only the condition panel open, all the others closed.
	ui.ClassicOEOpen = make([]bool, len(unit.ClassicOEHeaders))
	for i, header := range unit.ClassicOEHeaders {
		if header.Kind == unit.ClassicOEKindConditions {
			ui.ClassicOEOpen[i] = true
		}
	}

	var ops op.Ops
	gtx := testutil.NewLayoutContext(&ops, unit.ClassicOEPanelDp, 500)

	LayoutClassicOELeftPanel(ui, gtx)
	if !ui.ClassicOEScroll.HasBar {
		t.Fatalf("with the panel open the column has no scrollbar: content %d, viewport %d",
			ui.ClassicOEScroll.ContentHeight, ui.ClassicOEScroll.Viewport)
	}

	// Selecting a condition brings up its clause editor and its buttons.
	ui.ClassicOEConditionName = "cond_05"
	LayoutClassicOELeftPanel(ui, gtx)
	if !ui.ClassicOEScroll.HasBar {
		t.Errorf("selecting a condition took the column's scrollbar away: content %d, viewport %d",
			ui.ClassicOEScroll.ContentHeight, ui.ClassicOEScroll.Viewport)
	}
	if ui.ClassicOEScroll.ContentHeight <= ui.ClassicOEScroll.Viewport {
		t.Errorf("the column measures its content at the viewport itself (%d): the measure pass is being read as elastic content",
			ui.ClassicOEScroll.ContentHeight)
	}
}
