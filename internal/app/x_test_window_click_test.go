package app

import (
	"image"
	"strings"
	"testing"

	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
)

// A click in the file tree, in a whole window rather than in the panel alone.
//
// This is the test for the bug that made every click in the tree do nothing
// while a right press in the tree opened the tab bar's menu. The two targets
// had one tag between them — a zero sized allocation is the same pointer every
// time — so they shared one queue of events, and the tab bar's handler, which
// runs first, took the tree's press. Nothing about the tree or the tab bar was
// wrong; what was wrong could only be seen with the whole window on screen.

// windowFrame is the frame run() puts together: the input pass, then the
// layout, then the end of the frame.
type windowFrame struct {
	ui  *uiState
	r   input.Router
	ops op.Ops
	gtx layout.Context
}

func (f *windowFrame) frame() {
	f.HandleInput(ui, f.gtx)
	f.Layout(ui, f.gtx)
	f.r.Frame(&f.ops)
}

func (f *windowFrame) send(evs ...pointer.Event) {
	for _, e := range evs {
		f.r.Queue(e)
	}
}

// sendKey queues a key press for the next frame, the way the window queues what
// the keyboard delivered before the frame is drawn.
func (f *windowFrame) sendKey(evs ...key.Event) {
	for _, e := range evs {
		f.r.Queue(e)
	}
}

// newWindowFrame is a whole window over a state, with two frames run so that the
// targets on screen are the ones the router knows about.
func newWindowFrame(ui *uiState, w, h int) *windowFrame {
	f := &windowFrame{ui: ui}
	f.gtx = layout.Context{Ops: &f.ops, Source: f.r.Source(), Constraints: layout.Exact(image.Pt(w, h))}
	f.frame()
	f.frame()
	return f
}

// treeWindow is a whole window over a real mod, with one real file open so the
// tab bar and the properties panel are on screen the way they are in use.
func treeWindow(t *testing.T) (*windowFrame, int) {
	t.Helper()
	const target = `Data\attrib\ebps\races\necrons\troops\necron_basic_warrior.rgd`
	ui := newUI(unitTestTheme())
	ui.ModTree = openTestDXP3(t)
	ui.Expanded = map[string]bool{"Data": true}
	segs := strings.Split(target, `\`)
	for i := 2; i < len(segs); i++ {
		ui.Expanded[strings.Join(segs[:i], "\\")] = true
	}
	ui.VisibleNodes = GetVisibleNodes(ui)
	EnsureRowClicks(ui, len(ui.VisibleNodes))
	EnsureRowHover(ui, len(ui.VisibleNodes))
	row := -1
	for i, n := range ui.VisibleNodes {
		if n.FullPath == target {
			row = i
		}
	}
	if row < 0 {
		t.Skip("the target unit is not in the visible tree")
	}
	// A file that is not the one the tests click, so that a click which opens
	// nothing cannot pass by leaving the tab that was already there active.
	for _, n := range ui.VisibleNodes {
		if n.IsFile && strings.HasSuffix(n.Name, ".rgd") && n.Name != "necron_basic_warrior.rgd" {
			OpenTreeFile(ui, n)
			break
		}
	}
	if tab := ActiveTab(ui); tab == nil || strings.HasSuffix(tab.Path, "necron_basic_warrior.rgd") {
		t.Skip("the window could not be given a file that is not the one under test")
	}
	return newWindowFrame(ui, 900, 700), row
}

// treeOrigin is where the tree's own area starts in the window. An area
// reports positions in its own coordinates, and a press is the event that says
// where the area is: it comes back in the coordinates of the area it landed
// on. The press is a secondary one and is taken here and dropped, so the test
// that follows is the first thing to open any menu.
func treeOrigin(t *testing.T, f *windowFrame) f32.Point {
	t.Helper()
	// A whole frame first: the router only delivers to the targets of the
	// frame before it.
	f.frame()
	guess := f32.Pt(60, 400)
	f.send(
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: guess, Buttons: pointer.ButtonSecondary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: guess},
	)
	origin := f32.Pt(-1, -1)
	for {
		ev, ok := f.gtx.Event(pointer.Filter{Target: f.ui.LeftTreePointerTag, Kinds: pointer.Press})
		if !ok {
			break
		}
		if pe, ok := ev.(pointer.Event); ok {
			origin = f32.Pt(guess.X-pe.Position.X, guess.Y-pe.Position.Y)
		}
	}
	if origin.X < 0 {
		t.Fatalf("a press in the file tree was not delivered to the tree's own area")
	}
	return origin
}

func treeRowAt(t *testing.T, f *windowFrame, origin f32.Point, row int) f32.Point {
	t.Helper()
	ui := f.ui
	rowH := float32(ui.LeftRowHeightPx)
	y := origin.Y + float32(row)*rowH - float32(ui.LeftList.Position.Offset) + rowH/2
	return f32.Pt(origin.X+60, y)
}

// TestClickOnATreeRowOpensTheFileInAWholeWindow is the regression test: a click
// on a file in the tree opens it, with the tab bar and the properties panel on
// screen around it.
func TestClickOnATreeRowOpensTheFileInAWholeWindow(t *testing.T) {
	f, row := treeWindow(t)
	origin := treeOrigin(t, f)
	at := treeRowAt(t, f, origin, row)
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonPrimary},
		pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: at},
	)
	f.frame()
	tab := f.ActiveTab(ui)
	if tab == nil {
		t.Fatalf("a click on row %d at %v opened no tab; the status line reads %q", row, f32.Point(at), f.ui.StatusText)
	}
	if !strings.HasSuffix(tab.Path, "necron_basic_warrior.rgd") {
		t.Errorf("a click on row %d opened %q, want the file of that row", row, tab.Path)
	}
}

// TestRightPressOnATreeRowOpensTheFilesMenu is the other half of the same bug: a
// right press in the tree must open the tree's own menu, and must not open the
// tab bar's. It appeared over the tree because the two targets shared a tag.
func TestRightPressOnATreeRowOpensTheFilesMenu(t *testing.T) {
	f, row := treeWindow(t)
	origin := treeOrigin(t, f)
	at := treeRowAt(t, f, origin, row)
	f.send(
		pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at},
		pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: at, Buttons: pointer.ButtonSecondary},
	)
	f.frame()
	if f.ui.TabContextVisible {
		t.Errorf("a right press in the file tree opened the tab bar's menu")
	}
	if !f.ui.ContextMenuVisible {
		t.Fatalf("a right press on row %d opened no file menu", row)
	}
	if f.ui.ContextMenuPath != f.ui.VisibleNodes[row].FullPath {
		t.Errorf("the file menu is for %q, want %q", f.ui.ContextMenuPath, f.ui.VisibleNodes[row].FullPath)
	}
}

// TestEveryPointerTagOfTheAppIsItsOwn covers the cause rather than the symptom:
// a zero sized allocation is the same pointer every time, so two tags made that
// way are one tag, and two targets that share a tag share one queue of events.
func TestEveryPointerTagOfTheAppIsItsOwn(t *testing.T) {
	ui := newUI(unitTestTheme())
	if ui.LeftTreePointerTag == ui.TabBarPointerTag {
		t.Fatalf("the file tree and the tab bar share the pointer tag %v, so they share one queue of events", ui.TabBarPointerTag)
	}
	seen := map[event.Tag]string{}
	for name, tag := range map[string]event.Tag{
		"left tree": ui.LeftTreePointerTag,
		"tab bar":   ui.TabBarPointerTag,
		"locale 1":  newPointerTag("a"),
		"locale 2":  newPointerTag("b"),
	} {
		if tag == nil {
			t.Fatalf("the %s target has no tag", name)
		}
		if other, ok := seen[tag]; ok {
			t.Errorf("the %s and the %s targets are one tag, %v", name, other, tag)
		}
		seen[tag] = name
	}
}
