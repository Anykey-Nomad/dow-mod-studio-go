package cliphist_test

import (
	"dow-mod-studio/internal/cliphist"
	"fmt"
	"testing"
)

func TestClipHistoryKeepsNewestFiftyEntries(t *testing.T) {
	var h cliphist.ClipHistory
	for i := 0; i < cliphist.ClipHistoryLimit+5; i++ {
		h.Push(cliphist.ClipEntry{Kind: cliphist.ClipEvents, Label: fmt.Sprint(i)})
	}
	if len(h.Entries) != cliphist.ClipHistoryLimit {
		t.Fatalf("history holds %d Entries, want %d", len(h.Entries), cliphist.ClipHistoryLimit)
	}
	if got := h.Entries[0].Label; got != "5" {
		t.Errorf("oldest kept entry is %q, want %q", got, "5")
	}
	if got := h.Entries[len(h.Entries)-1].Label; got != fmt.Sprint(cliphist.ClipHistoryLimit+4) {
		t.Errorf("newest entry is %q, want %q", got, fmt.Sprint(cliphist.ClipHistoryLimit+4))
	}
}

func TestClipHistoryLatestFitsOnlyItsKind(t *testing.T) {
	var h cliphist.ClipHistory
	h.Push(cliphist.ClipEntry{Kind: cliphist.ClipEvents, Label: "events"})
	h.Push(cliphist.ClipEntry{Kind: cliphist.ClipBoneKeys, Label: "keys"})

	e, idx, ok := h.Latest(cliphist.ClipEvents)
	if !ok || e.Label != "events" || idx != 0 {
		t.Fatalf("latest(cliphist.ClipEvents) = %q at %d, ok=%v; want \"events\" at 0", e.Label, idx, ok)
	}
	if _, _, ok := h.Latest(cliphist.ClipImportKeys); ok {
		t.Error("latest(cliphist.ClipImportKeys) found an entry; no import keys were copied")
	}
}

func TestClipHistoryDropKindLeavesOtherKinds(t *testing.T) {
	var h cliphist.ClipHistory
	h.Push(cliphist.ClipEntry{Kind: cliphist.ClipEvents, Label: "a"})
	h.Push(cliphist.ClipEntry{Kind: cliphist.ClipBoneKeys, Label: "b"})
	h.Push(cliphist.ClipEntry{Kind: cliphist.ClipEvents, Label: "c"})

	h.DropKind(cliphist.ClipEvents)
	if len(h.Entries) != 1 || h.Entries[0].Label != "b" {
		t.Fatalf("after dropKind(cliphist.ClipEvents) Entries = %+v; want only the bone-key entry", h.Entries)
	}
}
