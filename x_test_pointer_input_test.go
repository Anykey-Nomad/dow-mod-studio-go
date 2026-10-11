package main

import (
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
)

// The pointer path, driven for real.
//
// Gio's router can be fed events from a test after all: a frame has to be
// closed first, so the ops of that frame are collected into the hit tree, and
// only a handler that asked for its events during a frame survives the end of
// it. Every test here therefore keeps to the order a window keeps — lay out,
// ask for events, close the frame — and the assertions are about what the
// handlers then do with what they were given.

// pointerFrame is one frame's worth of a window: the layout, the input pass
// and the end of the frame, in that order.
type pointerFrame struct {
	ui   *uiState
	r    input.Router
	ops  op.Ops
	gtx  layout.Context
	draw func(gtx layout.Context)
	read func(gtx layout.Context)
}

// newPointerFrame starts a frame loop of a window w by h that lays out with
// draw and reads its input with read.
func newPointerFrame(ui *uiState, w, h int, draw, read func(gtx layout.Context)) *pointerFrame {
	f := &pointerFrame{ui: ui, draw: draw, read: read}
	f.gtx = layout.Context{
		Ops:         &f.ops,
		Source:      f.r.Source(),
		Constraints: layout.Exact(image.Pt(w, h)),
	}
	return f
}

func (f *pointerFrame) frame() {
	f.draw(f.gtx)
	f.read(f.gtx)
	f.r.Frame(&f.ops)
}

// send queues events for the next frame, the way the window queues what the
// system delivered before the frame is drawn.
func (f *pointerFrame) send(evs ...pointer.Event) {
	for _, e := range evs {
		f.r.Queue(e)
	}
}

// treePointerUI is a state with a real mod behind it, one file expanded to and
// a pointer frame over the left panel, which is the only thing that registers
// the tree's pointer target.
func treePointerUI(t *testing.T) (*uiState, *pointerFrame, int) {
	t.Helper()
	const target = `Data\attrib\ebps\races\necrons\troops\necron_basic_warrior.rgd`
	ui := newUI(unitTestTheme())
	ui.modTree = openTestDXP3(t)
	ui.expanded = map[string]bool{"Data": true}
	segs := strings.Split(target, `\`)
	for i := 2; i < len(segs); i++ {
		ui.expanded[strings.Join(segs[:i], "\\")] = true
	}
	ui.visibleNodes = ui.getVisibleNodes()
	ui.ensureRowClicks(len(ui.visibleNodes))
	ui.ensureRowHover(len(ui.visibleNodes))
	row := -1
	for i, n := range ui.visibleNodes {
		if n.FullPath == target {
			row = i
		}
	}
	if row < 0 {
		t.Skip("the target unit is not in the visible tree")
	}
	f := newPointerFrame(ui, 300, 600, func(gtx layout.Context) { ui.layoutLeftPanel(gtx) }, func(gtx layout.Context) {
		ui.handleTreePointerInput(gtx, ui.visibleNodes)
	})
	// Two frames: the first one registers the target and hands the router the
	// one-off reset event it gives a new target, the second one a hit tree
	// that is settled.
	f.frame()
	f.frame()
	return ui, f, row
}

// treeRowPoint is where in the window a row is. A tree area reports positions
// in its own coordinates, and nothing but an event addressed to it says where
// that area begins, so a press is used to find out: it comes back in the
// coordinates of the area it landed on. The press is a secondary one and is
// taken here and dropped, so no file menu opens on it.
func treeRowPoint(t *testing.T, f *pointerFrame, row int) f32.Point {
	t.Helper()
	ui := f.ui
	guess := f32.Pt(60, 500)
	f.send(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: guess, Buttons: pointer.ButtonSecondary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: guess},
	)
	top := float32(-1)
	for {
		ev, ok := f.gtx.Event(pointer.Filter{Target: ui.leftTreePointerTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			top = guess.Y - pe.Position.Y
		}
	}
	if top < 0 {
		t.Fatalf("a press in the file tree was not delivered to the tree's own area")
	}
	rowH := float32(ui.leftRowHeightPx)
	y := top + float32(row)*rowH - float32(ui.leftList.Position.Offset) + rowH/2
	return f32.Pt(60, y)
}

// TestTreeClickOpensTheFile is the regression test for the reported bug: a
// click on a row did nothing, while Enter on the same row opened the file. The
// press and the release are separate events, and a tree row is only activated
// when the second one lands on the row the first one did.
func TestTreeClickOpensTheFile(t *testing.T) {
	ui, f, row := treePointerUI(t)
	at := treeRowPoint(t, f, row)
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
	tab := ui.activeTab()
	if tab == nil {
		t.Fatalf("a click on row %d opened no tab; the status line reads %q", row, ui.statusText)
	}
	if !strings.HasSuffix(tab.path, "necron_basic_warrior.rgd") {
		t.Errorf("a click on row %d opened %q, want the file of that row", row, tab.path)
	}
}

// TestTreeClickOnAnotherRowDoesNotOpen covers the other half of the pair: a
// press on one row and a release on another is a drag over the list, not a
// click, and must not open the row the press started on. The pointer moves in
// between, as a dragged one does, which is what tells Gio that the row under
// the press was left behind.
func TestTreeClickOnAnotherRowDoesNotOpen(t *testing.T) {
	ui, f, row := treePointerUI(t)
	rowH := float32(ui.leftRowHeightPx)
	at := treeRowPoint(t, f, row)
	other := at.Add(f32.Pt(0, rowH))
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: other},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: other},
	)
	f.frame()
	if tab := ui.activeTab(); tab != nil {
		t.Errorf("a press and a release on two rows opened %q, want nothing", tab.path)
	}
}

// TestTreeRowHoverFollowsThePointer checks the highlight that goes with the
// click: it is the row's own gesture that knows the pointer is over it, so it
// is the row's gesture that has to be asked.
func TestTreeRowHoverFollowsThePointer(t *testing.T) {
	ui, f, row := treePointerUI(t)
	at := treeRowPoint(t, f, row)
	f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
	f.frame()
	if !ui.rowHover[row] {
		t.Errorf("the pointer is over row %d and the row is not marked hovered", row)
	}
	if hovered := hoveredRows(ui); len(hovered) != 1 || hovered[0] != row {
		t.Errorf("hovered rows are %v, want only row %d", hovered, row)
	}
	f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at.Add(f32.Pt(0, float32(ui.leftRowHeightPx)))})
	f.frame()
	if ui.rowHover[row] {
		t.Errorf("the pointer has left row %d, which is still marked hovered", row)
	}
}

func hoveredRows(ui *uiState) []int {
	var rows []int
	for i, h := range ui.rowHover {
		if h {
			rows = append(rows, i)
		}
	}
	return rows
}

// TestTreeWheelScrollsUnderTheRows is the reason the rows' areas are pass
// through ones. The list's own wheel target is registered below the rows, and
// a row that took the pointer away from it would leave the tree unscrollable
// the moment a click landed in it.
func TestTreeWheelScrollsUnderTheRows(t *testing.T) {
	ui, f, row := treePointerUI(t)
	at := treeRowPoint(t, f, row)
	before := ui.leftList.Position.First
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Scroll, Source: pointer.Mouse, Position: at, Scroll: f32.Pt(0, 200)},
	)
	f.frame()
	f.frame()
	if ui.leftList.Position.First <= before {
		t.Fatalf("the wheel over row %d did not scroll the tree: first row is still %d", row, before)
	}
}

// tabBarPointerUI is a state with three tabs of the kinds the bar really gets,
// and a pointer frame over the bar alone.
func tabBarPointerUI(t *testing.T) (*uiState, *pointerFrame) {
	t.Helper()
	ui := newUI(unitTestTheme())
	ui.openTabs = append(ui.openTabs,
		&fileTab{kind: "rgd", path: `Data\attrib\ebps\races\space_marines\space_marine_orbital_relay.rgd`},
		&fileTab{kind: "rgd", path: `Data\attrib\ebps\races\space_marines\space_marine_turret_bolter.rgd`},
		&fileTab{kind: unitTabKind, path: unitTabPath},
	)
	ui.ensureTabClicks()
	ui.tabSel = 1
	f := newPointerFrame(ui, 900, 600, func(gtx layout.Context) { ui.layoutTabBar(gtx) }, ui.handleTabBarInput)
	f.frame()
	f.frame()
	return ui, f
}

// TestTabBarRightPressOpensTheContextMenu is the regression test for the tab
// bar's own target never being reached. Gio's hit test walks a frame's input
// stack from the top and stops at the first target it meets, so a target
// registered before the tabs is invisible wherever a tab is under the pointer:
// the wheel did nothing over a tab and the right press opened no menu.
func TestTabBarRightPressOpensTheContextMenu(t *testing.T) {
	ui, f := tabBarPointerUI(t)
	for i, r := range ui.tabRects {
		at := f32.Pt(float32((r.Min.X+r.Max.X)/2), float32((r.Min.Y+r.Max.Y)/2))
		f.send(
			pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
			pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonSecondary},
		)
		f.frame()
		if !ui.tabContextVisible {
			t.Fatalf("a right press on tab %d opened no context menu", i)
		}
		if ui.tabContextIndex != i {
			t.Errorf("a right press at %v opened the menu for tab %d, want tab %d", f32.Point(at), ui.tabContextIndex, i)
		}
		ui.closeTabContextMenu()
		f.frame()
	}
}

// TestTabBarTabsAreLaidOutSideBySide is the regression test for the tabs being
// drawn and hit-tested on top of one another at the row's left edge. Each tab
// is laid out under its own offset, so what is under the pointer is the tab
// whose rectangle says so — checked here through the close buttons, which are
// the one part of a tab that is at a known place inside it.
func TestTabBarTabsAreLaidOutSideBySide(t *testing.T) {
	ui, f := tabBarPointerUI(t)
	for i := range ui.tabRects {
		r := ui.tabRects[i]
		if r.Min.X >= r.Max.X {
			t.Fatalf("tab %d has no width: %v", i, r)
		}
		if i > 0 && r.Min.X <= ui.tabRects[i-1].Max.X {
			t.Errorf("tab %d starts at %d, which is not past the end of tab %d at %d", i, r.Min.X, i-1, ui.tabRects[i-1].Max.X)
		}
		// The close button sits at the right hand end of the tab, twelve
		// pixels in from its edge.
		at := f32.Pt(float32(r.Max.X)-12, float32((r.Min.Y+r.Max.Y)/2))
		f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
		f.frame()
		if !ui.tabCloses[i].Hovered() {
			t.Errorf("the pointer at %v is not over tab %d's close button, so the tabs are not laid out side by side", f32.Point(at), i)
		}
	}
}
