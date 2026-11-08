package uikit

import (
	"dow-mod-studio/internal/app/uistate"
	"dow-mod-studio/internal/cliphist"
	"fmt"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op/clip"
	"gioui.org/widget"
	"image"
	"math"
	"strconv"
	"time"
)

// The paste kinds and the name spaces are uistate's and cliphist's; they are
// re-exported here so a feature package wires a field or a row with the kit
// it already draws it with.
type ClipPasteType = uistate.ClipPasteType

const (
	ClipPasteNone  = uistate.ClipPasteNone
	ClipPasteTime  = uistate.ClipPasteTime
	ClipPasteFrame = uistate.ClipPasteFrame
	ClipPasteName  = uistate.ClipPasteName

	NameSpaceMotion    = cliphist.NameSpaceMotion
	NameSpaceAction    = cliphist.NameSpaceAction
	NameSpaceAnimation = cliphist.NameSpaceAnimation
	NameSpaceEvent     = cliphist.NameSpaceEvent
	NameSpaceMarker    = cliphist.NameSpaceMarker
	NameSpaceBone      = cliphist.NameSpaceBone
	NameSpaceSound     = cliphist.NameSpaceSound
	// Names copied from one .whe timeline, its sounds and its FX paths,
	// which are pasted into the matching edit fields beside an event.
	NameSpaceTimeline  = cliphist.NameSpaceTimeline
	NameSpaceSoundPath = cliphist.NameSpaceSoundPath
	NameSpaceFXPath    = cliphist.NameSpaceFXPath
)

// MarkClipPaste types a field for the frame/time clipboard: time fields take
// the moment's seconds, frame fields its frame number. It runs ahead of the
// layout that registers the field's press area — handleInput before Layout,
// the same order every other click in the app follows — and is idempotent, so
// a field is marked every frame it is on screen.
func MarkClipPaste(ui *uiState, ed *widget.Editor, typ ClipPasteType) {
	f := ensureClipField(ui, ed)
	f.Type = typ
}

// MarkClipPasteName types a field for copied names: only entries of the
// given spaces paste into it, so a motion-or-action field takes both and an
// event field neither.
func MarkClipPasteName(ui *uiState, ed *widget.Editor, spaces ...string) {
	f := ensureClipField(ui, ed)
	f.Type = uistate.ClipPasteName
	f.Spaces = spaces
}

// MarkClipPasteAny types a field for pasted names of any space — the motion-
// or-action line of an action, where the row above may be either and the
// clipboard carries one of them.
func MarkClipPasteAny(ui *uiState, ed *widget.Editor) {
	f := ensureClipField(ui, ed)
	f.Type = uistate.ClipPasteName
	f.Spaces = cliphist.AllNameSpaces()
}

func ensureClipField(ui *uiState, ed *widget.Editor) *uistate.ClipPasteField {
	if ui.ClipPasteFields == nil {
		ui.ClipPasteFields = make(map[*widget.Editor]*uistate.ClipPasteField)
	}
	f, ok := ui.ClipPasteFields[ed]
	if !ok {
		f = &uistate.ClipPasteField{Tag: NewPointerTag("clip-paste")}
		ui.ClipPasteFields[ed] = f
	}
	return f
}

// LayoutClipPaste registers the pointer area a typed field is right-clicked
// on, over the box of sz the caller just drew at the current origin. It sits
// inside a pass so the field's own caret hit-testing keeps working — the
// arrangement the tab bar uses for its right-click menu. An untyped field
// registers nothing, so every other editor in the app is untouched.
func LayoutClipPaste(ui *uiState, gtx layout.Context, ed *widget.Editor, sz image.Point) {
	f := ui.ClipPasteFields[ed]
	if f == nil || f.Type == uistate.ClipPasteNone {
		return
	}
	LayoutPointerArea(gtx, f.Tag, sz)
}

// LayoutPointerArea registers a bare pointer area of sz over the box just
// drawn at the current origin, in its own pass so what is underneath keeps
// its own hit-testing. It draws nothing.
func LayoutPointerArea(gtx layout.Context, tag event.Tag, sz image.Point) {
	if tag == nil || sz.X <= 0 || sz.Y <= 0 {
		return
	}
	pass := pointer.PassOp{}.Push(gtx.Ops)
	st := clip.Rect{Max: sz}.Push(gtx.Ops)
	event.Op(gtx.Ops, tag)
	st.Pop()
	pass.Pop()
}

// RowKey is the clipboard tag key of row i of a named list: layout and input
// name the same row the same way across frames, whatever the slices behind
// them do.
func RowKey(prefix string, i int) string {
	return prefix + "/" + strconv.Itoa(i)
}

// LayoutRowNameCopy registers the area whose right-press copies the name of
// the row at key — call it where the row is drawn, with the row's own box.
func LayoutRowNameCopy(ui *uiState, gtx layout.Context, key string, sz image.Point) {
	if ui.RowNameTags == nil {
		ui.RowNameTags = make(map[string]event.Tag)
	}
	tag := ui.RowNameTags[key]
	if tag == nil {
		tag = NewPointerTag("row-name")
		ui.RowNameTags[key] = tag
	}
	LayoutPointerArea(gtx, tag, sz)
}

// CopyRowName reads the right presses on the row at key and copies its name
// into the shared clipboard as a name entry of the given space — the motion,
// action, animation, event, sound, marker, timeline or FX list the name came
// from decides which fields take it. It reports whether it copied; the status
// line says so itself. Call it wherever the row's own click is read, in the
// same pass.
func CopyRowName(ui *uiState, gtx layout.Context, key, space, name string) bool {
	if ui.RowNameTags == nil {
		return false
	}
	tag := ui.RowNameTags[key]
	if tag == nil {
		return false
	}
	copied := false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || !e.Buttons.Contain(pointer.ButtonSecondary) {
			continue
		}
		ui.UnitClips.Push(cliphist.ClipEntry{
			Kind:  cliphist.ClipName,
			Label: fmt.Sprintf("%s %s", space, name),
			At:    time.Now(),
			Name:  &cliphist.NameClip{Space: space, Name: name},
		})
		ui.StatusText = fmt.Sprintf("Copied %s name %q — right-click a name field to paste", space, name)
		copied = true
	}
	return copied
}

// RowNameText registers the right-press area of the row of sz just drawn and
// reads it in the same call — for a list whose rows are built and read in one
// place rather than laid out in one pass and read in another. It reports
// whether it copied.
func RowNameText(ui *uiState, gtx layout.Context, key, space, name string, sz image.Point) bool {
	LayoutRowNameCopy(ui, gtx, key, sz)
	return CopyRowName(ui, gtx, key, space, name)
}

// PasteClipField reads the right presses on a typed field and pastes the
// clipboard's entry into it: the seconds into a time field, the frame number
// into a frame field, the newest name of a matching space into a name field.
// The text is replaced and the field takes the caret, ready to be submitted
// the way a typed value would be; the status line says what was pasted. It
// reports whether it pasted at all.
func PasteClipField(ui *uiState, gtx layout.Context, ed *widget.Editor) bool {
	f := ui.ClipPasteFields[ed]
	if f == nil || f.Type == uistate.ClipPasteNone {
		return false
	}
	pasted := false
	for {
		ev, ok := gtx.Event(pointer.Filter{Target: f.Tag, Kinds: pointer.Press})
		if !ok {
			break
		}
		e, ok := ev.(pointer.Event)
		if !ok || !e.Buttons.Contain(pointer.ButtonSecondary) {
			continue
		}
		switch f.Type {
		case uistate.ClipPasteName:
			entry, _, ok := ui.UnitClips.LatestName(f.Spaces)
			if !ok || entry.Name == nil {
				ui.StatusText = "Nothing to paste — right-click a list row to copy its name first"
				continue
			}
			ed.SetText(entry.Name.Name)
			ui.StatusText = fmt.Sprintf("Pasted %s name %q", entry.Name.Space, entry.Name.Name)
		case uistate.ClipPasteTime, uistate.ClipPasteFrame:
			// The history cursor belongs to the events panel, which steps it
			// with its own buttons. A time or frame field takes the newest
			// moment outright: going through the cursor let a stale position
			// on an old FrameTime entry pin every paste to the first value
			// ever copied.
			entry, _, ok := ui.UnitClips.Latest(cliphist.ClipFrameTime)
			if !ok || entry.FrameTime == nil {
				ui.StatusText = "Nothing to paste — right-click the timebar to copy a time first"
				continue
			}
			moment := entry.FrameTime
			// Whole milliseconds in the field: the strip measures in frames,
			// and a paste of 0.4666666666666667 is noise where 0.467 is a value.
			secs := strconv.FormatFloat(math.Round(moment.Time*1000)/1000, 'f', -1, 64)
			if f.Type == uistate.ClipPasteTime {
				ed.SetText(secs)
				ui.StatusText = fmt.Sprintf("Pasted %s s — frame %d of %s", secs, moment.Frame, moment.Anim)
			} else {
				text := strconv.Itoa(moment.Frame)
				ed.SetText(text)
				ui.StatusText = fmt.Sprintf("Pasted frame %s (%s s of %s)", text, secs, moment.Anim)
			}
		default:
			continue
		}
		gtx.Execute(key.FocusCmd{Tag: ed})
		pasted = true
	}
	return pasted
}
