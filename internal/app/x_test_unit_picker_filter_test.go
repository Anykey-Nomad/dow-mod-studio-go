package app

import (
	"dow-mod-studio/internal/testutil"
	"testing"

	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/op"
	"gioui.org/text"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// unitFilterFixture is an object tab over a mod whose units are spread over
// two races, so a search can be told apart from a list of one race.
func unitFilterFixture(t *testing.T) *uiState {
	t.Helper()
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	tree := testModTreeWithFiles(t, map[string]string{
		`Data\attrib\ebps\races\chaos\troops\chaos_lord.rgd`:        "",
		`Data\attrib\ebps\races\chaos\troops\chaos_spawn.rgd`:       "",
		`Data\attrib\ebps\races\necrons\troops\necron_lord.rgd`:     "",
		`Data\attrib\ebps\races\necrons\troops\necron_warrior.rgd`:  "",
		`Data\attrib\ebps\races\necrons\structures\necron_tomb.rgd`: "",
	})
	ui := newUI(th)
	ui.ModTree = tree
	OpenUnitTab(ui)
	return ui
}

// The unit list is a search as well as a list: with the filter empty it holds
// the race on show, and with text in it every race, so a unit can be found
// without knowing which race holds it. Each row says which race and which unit
// it stands for, because picking one has to move the selection to where the
// unit actually is.
func TestTheUnitFilterSearchesEveryRace(t *testing.T) {
	ui := unitFilterFixture(t)
	cat := UnitCatalogFor(ui)
	if cat == nil || len(cat.Races) < 2 {
		t.Fatalf("the fixture has %d races, so there is nothing to search across", len(cat.Races))
	}
	necrons := cat.FindRace("necrons")
	if necrons == nil {
		t.Fatal("the fixture lost a race")
	}
	chaosIdx, necronsIdx := -1, -1
	for i := range cat.Races {
		switch cat.Races[i].Name {
		case "chaos":
			chaosIdx = i
		case "necrons":
			necronsIdx = i
		}
	}
	if chaosIdx < 0 || necronsIdx < 0 {
		t.Fatalf("the fixture's races are %v, neither of the two it was built with", cat.Races)
	}

	// With the filter empty the list is the race on show, in its own order.
	ui.UnitRaceSel = necronsIdx
	ui.UnitSel = 0
	hits := UnitPickerHits(ui)
	if want := necrons.UnitCount(); len(hits) != want {
		t.Fatalf("the unfiltered list holds %d rows, want the %d of the race on show", len(hits), want)
	}
	for i, hit := range hits {
		if hit.race != necronsIdx || hit.unit != i {
			t.Fatalf("row %d is race %d unit %d, want race %d unit %d", i, hit.race, hit.unit, necronsIdx, i)
		}
	}

	// With text, every race is searched: a chaos unit is found while the
	// race on show is necrons.
	ui.UnitPickerFilter.SetText("chaos_lord")
	hits = UnitPickerHits(ui)
	if len(hits) != 1 {
		t.Fatalf("\"chaos_lord\" found %d units, want 1: %v", len(hits), hits)
	}
	if hits[0].race != chaosIdx {
		t.Errorf("the hit stands for race %d, want chaos at %d", hits[0].race, chaosIdx)
	}
	u, ok := unitPickerUnitAt(&cat.Races[hits[0].race], hits[0].unit)
	if !ok || u.Name != "chaos_lord" {
		t.Errorf("the hit resolves to %q, want chaos_lord", u.Name)
	}

	// A name of another race's unit, and a race's own name, both narrow the
	// list to that race.
	ui.UnitPickerFilter.SetText("necron")
	hits = UnitPickerHits(ui)
	if len(hits) != necrons.UnitCount() {
		t.Errorf("\"necron\" found %d units, want the %d of necrons", len(hits), necrons.UnitCount())
	}
	for _, hit := range hits {
		if hit.race != necronsIdx {
			t.Errorf("a necron hit stands for race %d", hit.race)
		}
	}

	// A filter nothing matches leaves no rows rather than the whole list.
	ui.UnitPickerFilter.SetText("no_unit_is_called_this")
	if hits = UnitPickerHits(ui); len(hits) != 0 {
		t.Errorf("a filter nothing matches found %d units", len(hits))
	}
	// And clearing it puts the race's own list back.
	ui.UnitPickerFilter.SetText("")
	if hits = UnitPickerHits(ui); len(hits) != necrons.UnitCount() {
		t.Errorf("clearing the filter left %d rows, want the race's %d", len(hits), necrons.UnitCount())
	}
}

// The filter is part of the dropdown's own box: the rows hang below it, so the
// list the dropdown has always been is now that much taller rather than having
// its first rows pushed under the filter.
func TestTheUnitListDrawsItsFilterAboveItsRows(t *testing.T) {
	ui := unitFilterFixture(t)
	ui.UnitListOpen = true
	const w, h = 900, 700
	var ops op.Ops
	LayoutUnitTabBody(ui, testutil.NewLayoutContext(&ops, w, h))
	box := ui.UnitDropBox
	if box.Empty() {
		t.Fatal("the layout reported no dropdown box at all")
	}
	// The test metric is one pixel per dp, so the box is the filter row plus
	// the rows it shows, unless the window is too short to hold them.
	want := unitPickerFilterDp + unitListVisibleRows*unitRowHeightDp
	if box.Dy() != want {
		t.Errorf("the dropdown is %d px tall, want the filter's %d and the rows' %d", box.Dy(), unitPickerFilterDp, unitListVisibleRows*unitRowHeightDp)
	}
}

// findUnitFilterRow walks the pointer over the open dropdown until a row
// reports itself under it, and returns where that was. The coarse step is
// under the height of a row and well under its width, so no row can be
// stepped over.
func findUnitFilterRow(t *testing.T, f *windowFrame, row *widget.Clickable) f32.Point {
	t.Helper()
	const w, h = 900, 700
	for y := 40; y < h-40 && y < 520; y += 12 {
		for x := 20; x < 560; x += 16 {
			at := f32.Pt(float32(x), float32(y))
			f.send(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: at})
			f.frame()
			if row.Hovered() {
				return at
			}
		}
	}
	t.Fatalf("no row of the open unit list is on screen at this size")
	return f32.Point{}
}

// Picking a row the filter found moves the selection to where the unit
// actually is — the race with it, when the unit belongs to another one — and
// puts the list away.
func TestPickingAFilteredRowSwitchesToThatUnit(t *testing.T) {
	ui := unitFilterFixture(t)
	cat := UnitCatalogFor(ui)
	if cat == nil || len(cat.Races) < 2 {
		t.Fatal("the fixture needs two races")
	}
	necronsIdx := -1
	for i := range cat.Races {
		if cat.Races[i].Name == "necrons" {
			necronsIdx = i
		}
	}
	necrons := cat.FindRace("necrons")
	wantUnit := -1
	for i, u := range necrons.UnitsOf() {
		if u.Name == "necron_warrior" {
			wantUnit = i
		}
	}
	if necronsIdx < 0 || wantUnit < 0 {
		t.Fatal("the fixture lost the unit this test picks")
	}
	// The race on show is another one, so only the search can find it.
	ui.UnitRaceSel = 0
	ui.UnitSel = 0
	ui.UnitListOpen = true
	f := newWindowFrame(ui, 900, 700)
	filter := RgdEnsureEditorClick(ui, &ui.UnitPickerFilter)
	at := findUnitFilterRow(t, f, &filter.Click)
	clickAt(f, at)
	f.r.Queue(key.EditEvent{Text: "necron_warrior"})
	f.frame()
	if got := ui.UnitPickerFilter.Text(); got != "necron_warrior" {
		t.Fatalf("filter text is %q, want necron_warrior", got)
	}
	if hits := UnitPickerHits(ui); len(hits) != 1 {
		t.Fatalf("the filter offers %d rows, want the one it found: %v", len(hits), hits)
	}
	at = findUnitFilterRow(t, f, &ui.UnitClicks[0])
	clickAt(f, at)
	if ui.UnitListOpen {
		t.Error("the list stayed open after a row was picked")
	}
	if ui.UnitRaceSel != necronsIdx {
		t.Errorf("the race is %d, want necrons at %d", ui.UnitRaceSel, necronsIdx)
	}
	if ui.UnitSel != wantUnit {
		t.Errorf("the unit is %d, want %d", ui.UnitSel, wantUnit)
	}
	if got := CurrentUnit(ui); got == nil || got.Name != "necron_warrior" {
		t.Errorf("the unit on show is %v, want necron_warrior", got)
	}
}
