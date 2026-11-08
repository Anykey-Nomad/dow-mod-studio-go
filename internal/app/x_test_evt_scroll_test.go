package app

import (
	"dow-mod-studio/internal/app/uikit"
	"dow-mod-studio/internal/testutil"
	"dow-mod-studio/internal/whe"
	"fmt"
	"image"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/unit"
)

// eventsSectionUI is the Events section with a .whe behind it whose two lists
// are long enough to scroll: 62 timelines and 25 actions, so both the list and
// the scrollbar under it have a real range to cover.
func eventsSectionUI(t *testing.T, w, h int) *uiState {
	t.Helper()
	ui, tree := unitEditUI(t)
	testutil.WriteTreeFile(t, tree, unitEventsTestWhePath, syntheticEventWhe())
	tree.SortAll(tree.Root)
	ui.UnitSection = unitSectionNamed("Events")
	var boot op.Ops
	LayoutUnitBody(ui, testutil.NewLayoutContext(&boot, w, h))
	if ui.UnitEvtDoc == nil {
		t.Fatalf("the section did not load a .whe while drawing: %s", ui.UnitEvtErr)
	}
	doc := ui.UnitEvtDoc
	var payload []byte
	for _, tl := range doc.Timelines() {
		if p, ok := doc.TimelinePayload(tl.Name); ok && len(p) > 0 {
			payload = p
			break
		}
	}
	if payload == nil {
		t.Fatal("no timeline payload to copy")
	}
	for i := 0; i < 60; i++ {
		if err := doc.AddTimeline(fmt.Sprintf("filter_tl_%02d", i), payload); err != nil {
			t.Fatalf("AddTimeline: %v", err)
		}
	}
	for i := 0; i < 25; i++ {
		if err := doc.AddAction(whe.FxAction{
			Name:    fmt.Sprintf("filter_act_%02d", i),
			Motions: []whe.FxActionLink{{Compare: whe.FxCompareIf, Name: "filter_tl_00"}},
		}); err != nil {
			t.Fatalf("AddAction: %v", err)
		}
	}
	return ui
}

// TestTheTimelineListScrollbarDragsProportionally is the test for the drag that
// charged one row of scroll per row-height of pointer travel: dragging the thumb
// half down the track moved the list a fifth of the way, so the end of a long
// list could not be reached with the scrollbar in either direction — and the
// rows at each end stayed out of sight no matter how far the thumb was pulled.
//
// It also checks that the filter above the list narrows it, since the rows it
// keeps are the ones the scrollbar has to cover.
func TestTheTimelineListScrollbarDragsProportionally(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)

	var r input.Router
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Source:      r.Source(),
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	frame := func() {
		LayoutUnitBody(ui, gtx)
		r.Frame(&ops)
	}
	frame()
	frame()

	// The scrollbar strip sits at the right edge of the middle column, and
	// its own top is where the list begins under the filter field.
	gap := gtx.Dp(unit.Dp(unitColGapDp))
	leftW, midW, _ := unitColumnWidths(w, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, gtx.Dp(unit.Dp(unitColMinWidthDp)))
	barX := float32(leftW + gap + midW - gtx.Dp(uikit.PanelScrollbarWidthDp)/2)
	listTop := -1
	for y := 40; y < 400 && listTop < 0; y += 4 {
		r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(barX, float32(y)), Buttons: pointer.ButtonPrimary})
		frame()
		if ui.UnitEvtScroll.Dragging {
			listTop = y
			break
		}
		r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(barX, float32(y))})
		frame()
	}
	if listTop < 0 {
		t.Fatal("the scrollbar strip was not found down the right edge of the list")
	}
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(barX, float32(listTop))})
	frame()

	// The filter above the list narrows the rows the list covers.
	unfiltered := ui.UnitEvtList.Position.Length
	ui.UnitEvtTlFilter.SetText("filter_tl_1")
	frame()
	filtered := ui.UnitEvtList.Position.Length
	if filtered >= unfiltered {
		t.Errorf("the timeline filter did not narrow the list: content %d after the filter, %d before", filtered, unfiltered)
	}
	ui.UnitEvtTlFilter.SetText("")
	frame()

	// From the top of the list, drag the thumb half down the track.
	ui.UnitEvtList.Position.First = 0
	ui.UnitEvtList.Position.Offset = 0
	frame()
	viewport := ui.UnitEvtScroll.Viewport
	content := ui.UnitEvtScroll.ContentHeight
	thumb := uikit.PanelScrollThumbSize(&ui.UnitEvtScroll, viewport)
	track := viewport - thumb
	total := content - viewport
	rowH := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
	if track <= 0 || total <= 0 {
		t.Fatalf("viewport=%d content=%d thumb=%d: the list has no scroll range", viewport, content, thumb)
	}
	startY := float32(listTop + 2)
	targetY := float32(listTop + viewport/2)
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(barX, startY), Buttons: pointer.ButtonPrimary})
	frame()
	if !ui.UnitEvtScroll.Dragging {
		t.Fatalf("a press at (%.0f,%.0f) did not land on the scrollbar", barX, startY)
	}
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(barX, targetY)})
	frame()
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(barX, targetY)})
	frame()

	delta := float64(targetY - startY)
	want := int(delta/float64(track)*float64(total) + .5)
	if want < 0 {
		want = 0
	}
	if want > total {
		want = total
	}
	got := ui.UnitEvtList.Position.First*rowH + ui.UnitEvtList.Position.Offset
	if diff := got - want; diff > 2*rowH || diff < -2*rowH {
		t.Errorf("dragging the thumb %.0fpx down a %.0fpx track moved the list %dpx, want %dpx (off by %d)",
			delta, float64(track), got, want, diff)
	}

	// And back up: the same drag in reverse has to return to the first row.
	r.Queue(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: f32.Pt(barX, targetY), Buttons: pointer.ButtonPrimary})
	frame()
	r.Queue(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: f32.Pt(barX, float32(listTop+1))})
	frame()
	r.Queue(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: f32.Pt(barX, float32(listTop+1))})
	frame()
	if back := ui.UnitEvtList.Position.First*rowH + ui.UnitEvtList.Position.Offset; back > 2*rowH {
		t.Errorf("dragging the thumb back to the top of the track left the list %dpx down, want the first row at the top", back)
	}
}

// TestTheTimelineListThumbReachesTheEndOfItsTrack covers a display scaled away
// from 100%. The rows are laid out in scaled pixels, while the row height the
// scrollbar was handed stays 22dp and Position.Offset is already pixels, so
// the thumb mirrored the list with the wrong pixel step. The list itself still
// scrolled to its last row — the wheel belongs to the list and works in
// pixels — but the thumb stopped short of the bottom of its own track, which
// reads as a list that will not scroll to the end.
func TestTheTimelineListThumbReachesTheEndOfItsTrack(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)

	var r input.Router
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Source:      r.Source(),
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1.25, PxPerSp: 1.25},
	}
	frame := func() {
		LayoutUnitBody(ui, gtx)
		r.Frame(&ops)
	}
	frame()
	frame()

	gap := gtx.Dp(unit.Dp(unitColGapDp))
	leftW, midW, _ := unitColumnWidths(w, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, gtx.Dp(unit.Dp(unitColMinWidthDp)))
	x := float32(leftW + gap + midW/2)

	// Wheel over the middle column until the timeline list itself moves: the
	// list takes the gesture over its own rectangle, so this scan finds that
	// rectangle the way the drag test above finds the scrollbar strip.
	at := 0
	for y := 150; y < h-10 && at == 0; y += 8 {
		at = y
		r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(x, float32(y)), Scroll: f32.Pt(0, 4000)})
		frame()
		if ui.UnitEvtList.Position.First > 0 {
			break
		}
		at = 0
	}
	if at == 0 {
		t.Fatal("a wheel over the middle column did not move the timeline list")
	}

	// Keep wheeling until the list stops moving: that is its last row.
	for i := 0; i < 80; i++ {
		before := ui.UnitEvtList.Position.First
		r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(x, float32(at)), Scroll: f32.Pt(0, 4000)})
		frame()
		if ui.UnitEvtList.Position.First == before {
			break
		}
	}

	rowPx := gtx.Dp(unit.Dp(unitRowHeightDp - 4))
	rows := ui.UnitEvtScroll.ContentHeight / rowPx
	if got := ui.UnitEvtList.Position.First + ui.UnitEvtList.Position.Count; got < rows-1 {
		t.Fatalf("the wheel stopped short of the end: row %d of %d laid out", got, rows)
	}
	state := ui.UnitEvtScroll
	want := state.ContentHeight - state.Viewport
	if want <= 0 {
		t.Fatalf("viewport=%d content=%d: the list has no scroll range", state.Viewport, state.ContentHeight)
	}
	if short := want - state.Offset; short > 2 {
		t.Errorf("with the list at its last row the thumb stops %dpx short of the bottom of its track (offset %d of %d)",
			short, state.Offset, want)
	}
}

// TestTheActionsFilterStaysPinnedAboveItsList covers the filter over the
// action rows scrolling away with them. The timeline block pins its header,
// its copy row and its filter above its own list; the actions block used to
// keep all three inside the scrolling content, so once the rows had moved the
// filter could only be reached by scrolling back — the copy buttons went with
// it.
//
// The check is the block's own arithmetic: laid out with room to fill, it is
// header, copy row, filter and rows together; in the column only the rows
// scroll. The difference between the two is the pinned part.
func TestTheActionsFilterStaysPinnedAboveItsList(t *testing.T) {
	const w, h = 1400, 800
	ui := eventsSectionUI(t, w, h)

	// Enough actions that the block is capped by the column and its rows
	// have to scroll: with only the fixture's twenty-five the rows fit and
	// there would be nothing for the filter to scroll away from.
	doc := ui.UnitEvtDoc
	for i := 0; i < 60; i++ {
		if err := doc.AddAction(whe.FxAction{
			Name:    fmt.Sprintf("pin_act_%02d", i),
			Motions: []whe.FxActionLink{{Compare: whe.FxCompareIf, Name: "filter_tl_00"}},
		}); err != nil {
			t.Fatalf("AddAction: %v", err)
		}
	}

	var r input.Router
	var ops op.Ops
	gtx := layout.Context{
		Ops:         &ops,
		Source:      r.Source(),
		Constraints: layout.Exact(image.Pt(w, h)),
		Metric:      unit.Metric{PxPerDp: 1, PxPerSp: 1},
	}
	frame := func() {
		LayoutUnitBody(ui, gtx)
		r.Frame(&ops)
	}
	frame()
	frame()

	gap := gtx.Dp(unit.Dp(unitColGapDp))
	leftW, midW, _ := unitColumnWidths(w, gtx.Dp(unit.Dp(unitLeftColumnDp)), gap, gtx.Dp(unit.Dp(unitColMinWidthDp)))
	x := float32(leftW + gap + midW/2)

	// The block laid out on its own with the whole window's height to fill:
	// header, copy row, filter and every row together.
	var mops op.Ops
	mgtx := gtx.Disabled()
	mgtx.Ops = &mops
	mgtx.Constraints = layout.Constraints{Max: image.Pt(w, panelScrollMeasureHeight)}
	whole := LayoutUnitEvtActions(ui, mgtx, doc.Actions()).Size.Y

	// What of that block lies outside its scrolling region is what stays
	// pinned: only the rows scroll, so the scroll state measures the rows
	// alone and the difference is the header, the copy row and the filter —
	// about ninety pixels of them. A block that scrolls the whole content,
	// as this one used to, keeps nothing outside and the difference is zero.
	pinned := whole - ui.UnitEvtActionsScroll.ContentHeight
	if pinned < 80 {
		t.Errorf("the block keeps only %dpx outside its scrolling region (whole %d, scrolling %d): the header, the copy row and the filter have to stay pinned above the rows",
			pinned, whole, ui.UnitEvtActionsScroll.ContentHeight)
	}

	// Wheel over the rows at the foot of the column — the region is the
	// block's last one — until the list has really moved.
	for y := h - 300; y < h-8 && ui.UnitEvtActionsScroll.Offset <= 100; y += 6 {
		r.Queue(pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: f32.Pt(x, float32(y)), Scroll: f32.Pt(0, 400)})
		frame()
	}
	if ui.UnitEvtActionsScroll.Offset <= 100 {
		t.Fatalf("the actions list did not scroll: offset %d of %d",
			ui.UnitEvtActionsScroll.Offset, ui.UnitEvtActionsScroll.ContentHeight-ui.UnitEvtActionsScroll.Viewport)
	}
}
