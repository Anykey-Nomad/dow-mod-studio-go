package main

import (
	"fmt"
	"time"
)

// clipHistoryLimit is how many copies the clipboard remembers. The oldest is
// dropped first once the limit is reached.
const clipHistoryLimit = 50

// clipKind says what a clipboard entry holds, and therefore which fields
// accept it on paste. An entry is only ever pasted into a field of its own
// kind: events never land on a keyframe track, keyframes never on a timeline.
type clipKind int

const (
	// clipEvents is a set of timeline events of a .whe, with the definitions
	// they name.
	clipEvents clipKind = iota
	// clipBoneKeys is the position and rotation keys of one bone.
	clipBoneKeys
	// clipImportKeys is keys of one bone read from an imported .whm.
	clipImportKeys
)

// String is the tag shown beside an entry in the history.
func (k clipKind) String() string {
	switch k {
	case clipEvents:
		return "Events"
	case clipBoneKeys:
		return "Bone keys"
	case clipImportKeys:
		return "Import keys"
	}
	return "Unknown"
}

// boneKeyClip is keys of one bone's track: position and rotation keys, each
// with the time it sat at in the clip it was copied from.
type boneKeyClip struct {
	pos []WhmKey3
	rot []WhmKey4
}

// clipEntry is one copy. The payload is typed by kind: only the field that
// matches kind is set, so a paste reads the one it expects and nothing else.
type clipEntry struct {
	kind  clipKind
	label string
	at    time.Time

	events *wheEventClip
	keys   *boneKeyClip
}

// fits reports whether the entry may be pasted into a field that takes the
// given kind.
func (e clipEntry) fits(target clipKind) bool {
	return e.kind == target
}

// clipHistory is the list of copies, oldest first.
type clipHistory struct {
	entries []clipEntry
}

// push appends a copy and drops the oldest entries beyond the limit.
func (h *clipHistory) push(e clipEntry) {
	h.entries = append(h.entries, e)
	if over := len(h.entries) - clipHistoryLimit; over > 0 {
		h.entries = append([]clipEntry(nil), h.entries[over:]...)
	}
}

// latest returns the newest entry that fits the target kind, with its index
// in the history. It reports false when no entry fits.
func (h *clipHistory) latest(target clipKind) (clipEntry, int, bool) {
	for i := len(h.entries) - 1; i >= 0; i-- {
		if h.entries[i].fits(target) {
			return h.entries[i], i, true
		}
	}
	return clipEntry{}, -1, false
}

// dropKind removes every entry of one kind. Used when the data it refers to
// is no longer valid, such as event definitions of another .whe.
func (h *clipHistory) dropKind(kind clipKind) {
	kept := h.entries[:0]
	for _, e := range h.entries {
		if e.kind != kind {
			kept = append(kept, e)
		}
	}
	h.entries = kept
}

// fitting lists the indices of the entries that fit the target kind, oldest
// first.
func (h *clipHistory) fitting(target clipKind) []int {
	var out []int
	for i, e := range h.entries {
		if e.fits(target) {
			out = append(out, i)
		}
	}
	return out
}

// pick is the entry a paste into the target takes: the one the history is
// positioned on when it fits, else the newest that fits.
func (h *clipHistory) pick(target clipKind, cur int) (clipEntry, int, bool) {
	if cur >= 0 && cur < len(h.entries) && h.entries[cur].fits(target) {
		return h.entries[cur], cur, true
	}
	return h.latest(target)
}

// label says what a paste into the target would take, and where that entry
// sits among the entries that fit.
func (h *clipHistory) label(target clipKind, cur int) string {
	e, idx, ok := h.pick(target, cur)
	if !ok {
		return "History: nothing to paste"
	}
	fit := h.fitting(target)
	n := 0
	for k, i := range fit {
		if i == idx {
			n = k + 1
		}
	}
	return fmt.Sprintf("History · %s · %s (%d/%d)", e.kind, e.label, n, len(fit))
}

// step moves the position to the next (dir > 0) or previous (dir < 0) entry
// that fits the target, staying on the end it is at. It returns the new
// position, or -1 when nothing fits.
func (h *clipHistory) step(target clipKind, cur, dir int) int {
	idx := h.fitting(target)
	if len(idx) == 0 {
		return -1
	}
	pos := len(idx) - 1
	for k, i := range idx {
		if i == cur {
			pos = k
		}
	}
	if dir > 0 && pos < len(idx)-1 {
		pos++
	} else if dir < 0 && pos > 0 {
		pos--
	}
	return idx[pos]
}
