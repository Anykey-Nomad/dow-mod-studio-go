package cliphist

import (
	"fmt"
	"time"

	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
)

// clipHistoryLimit is how many copies the clipboard remembers. The oldest is
// dropped first once the limit is reached.
const ClipHistoryLimit = 50

// clipKind says what a clipboard entry holds, and therefore which fields
// accept it on paste. An entry is only ever pasted into a field of its own
// kind: events never land on a keyframe track, keyframes never on a timeline.
type ClipKind int

const (
	// clipEvents is a set of timeline events of a .whe, with the definitions
	// they name.
	ClipEvents ClipKind = iota
	// clipBoneKeys is the position and rotation keys of one bone.
	ClipBoneKeys
	// clipImportKeys is keys of one bone read from an imported .whm.
	ClipImportKeys
	// clipActions is one action of a .whe's DATAACTS section: the chain of
	// motions it plays and the other actions it calls.
	ClipActions
)

// String is the tag shown beside an entry in the history.
func (k ClipKind) String() string {
	switch k {
	case ClipEvents:
		return "Events"
	case ClipBoneKeys:
		return "Bone keys"
	case ClipImportKeys:
		return "Import keys"
	case ClipActions:
		return "Actions"
	}
	return "Unknown"
}

// boneKeyClip is keys of one bone's track: position and rotation keys, each
// with the time it sat at in the clip it was copied from.
type BoneKeyClip struct {
	Pos []whm.Key3
	Rot []whm.Key4
}

// clipEntry is one copy. The payload is typed by kind: only the field that
// matches kind is set, so a paste reads the one it expects and nothing else.
type ClipEntry struct {
	Kind  ClipKind
	Label string
	At    time.Time

	Events  *WheEventClip
	Keys    *BoneKeyClip
	Actions *WheActionClip
}

// fits reports whether the entry may be pasted into a field that takes the
// given kind.
func (e ClipEntry) Fits(target ClipKind) bool {
	return e.Kind == target
}

// clipHistory is the list of copies, oldest first.
type ClipHistory struct {
	Entries []ClipEntry
}

// push appends a copy and drops the oldest entries beyond the limit.
func (h *ClipHistory) Push(e ClipEntry) {
	h.Entries = append(h.Entries, e)
	if over := len(h.Entries) - ClipHistoryLimit; over > 0 {
		h.Entries = append([]ClipEntry(nil), h.Entries[over:]...)
	}
}

// latest returns the newest entry that fits the target kind, with its index
// in the history. It reports false when no entry fits.
func (h *ClipHistory) Latest(target ClipKind) (ClipEntry, int, bool) {
	for i := len(h.Entries) - 1; i >= 0; i-- {
		if h.Entries[i].Fits(target) {
			return h.Entries[i], i, true
		}
	}
	return ClipEntry{}, -1, false
}

// dropKind removes every entry of one kind. Used when the data it refers to
// is no longer valid, such as event definitions of another .whe.
func (h *ClipHistory) DropKind(Kind ClipKind) {
	kept := h.Entries[:0]
	for _, e := range h.Entries {
		if e.Kind != Kind {
			kept = append(kept, e)
		}
	}
	h.Entries = kept
}

// fitting lists the indices of the entries that fit the target kind, oldest
// first.
func (h *ClipHistory) Fitting(target ClipKind) []int {
	var out []int
	for i, e := range h.Entries {
		if e.Fits(target) {
			out = append(out, i)
		}
	}
	return out
}

// pick is the entry a paste into the target takes: the one the history is
// positioned on when it fits, else the newest that fits.
func (h *ClipHistory) Pick(target ClipKind, cur int) (ClipEntry, int, bool) {
	if cur >= 0 && cur < len(h.Entries) && h.Entries[cur].Fits(target) {
		return h.Entries[cur], cur, true
	}
	return h.Latest(target)
}

// label says what a paste into the target would take, and where that entry
// sits among the entries that fit.
func (h *ClipHistory) Label(target ClipKind, cur int) string {
	e, idx, ok := h.Pick(target, cur)
	if !ok {
		return "History: nothing to paste"
	}
	fit := h.Fitting(target)
	n := 0
	for k, i := range fit {
		if i == idx {
			n = k + 1
		}
	}
	return fmt.Sprintf("History · %s · %s (%d/%d)", e.Kind, e.Label, n, len(fit))
}

// step moves the position to the next (dir > 0) or previous (dir < 0) entry
// that fits the target, staying on the end it is at. It returns the new
// position, or -1 when nothing fits.
func (h *ClipHistory) Step(target ClipKind, cur, dir int) int {
	idx := h.Fitting(target)
	if len(idx) == 0 {
		return -1
	}
	Pos := len(idx) - 1
	for k, i := range idx {
		if i == cur {
			Pos = k
		}
	}
	if dir > 0 && Pos < len(idx)-1 {
		Pos++
	} else if dir < 0 && Pos > 0 {
		Pos--
	}
	return idx[Pos]
}

// wheEventClip is the payload of a copied run of timeline events: the
// animations they name, the bytes they came in, and their definitions, so a
// paste into another unit can rewrite the animation names rather than fail.
type WheEventClip struct {
	Timeline   string
	Animations []string
	Payload    []byte
	Events     []whe.FxTimelineEvent
	Defs       map[string]whe.FxEvent
}

// wheActionClip is a copied action: the action itself, the timelines it
// plays and the sub-actions it calls, so a paste can rename what it must.
type WheActionClip struct {
	Action    whe.FxAction
	Timelines []WheEventClip
	Subacts   []whe.FxAction
}
