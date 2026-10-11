package main

import (
	"fmt"
	"testing"
)

func TestClipHistoryKeepsNewestFiftyEntries(t *testing.T) {
	var h clipHistory
	for i := 0; i < clipHistoryLimit+5; i++ {
		h.push(clipEntry{kind: clipEvents, label: fmt.Sprint(i)})
	}
	if len(h.entries) != clipHistoryLimit {
		t.Fatalf("history holds %d entries, want %d", len(h.entries), clipHistoryLimit)
	}
	if got := h.entries[0].label; got != "5" {
		t.Errorf("oldest kept entry is %q, want %q", got, "5")
	}
	if got := h.entries[len(h.entries)-1].label; got != fmt.Sprint(clipHistoryLimit+4) {
		t.Errorf("newest entry is %q, want %q", got, fmt.Sprint(clipHistoryLimit+4))
	}
}

func TestClipHistoryLatestFitsOnlyItsKind(t *testing.T) {
	var h clipHistory
	h.push(clipEntry{kind: clipEvents, label: "events"})
	h.push(clipEntry{kind: clipBoneKeys, label: "keys"})

	e, idx, ok := h.latest(clipEvents)
	if !ok || e.label != "events" || idx != 0 {
		t.Fatalf("latest(clipEvents) = %q at %d, ok=%v; want \"events\" at 0", e.label, idx, ok)
	}
	if _, _, ok := h.latest(clipImportKeys); ok {
		t.Error("latest(clipImportKeys) found an entry; no import keys were copied")
	}
}

func TestClipHistoryDropKindLeavesOtherKinds(t *testing.T) {
	var h clipHistory
	h.push(clipEntry{kind: clipEvents, label: "a"})
	h.push(clipEntry{kind: clipBoneKeys, label: "b"})
	h.push(clipEntry{kind: clipEvents, label: "c"})

	h.dropKind(clipEvents)
	if len(h.entries) != 1 || h.entries[0].label != "b" {
		t.Fatalf("after dropKind(clipEvents) entries = %+v; want only the bone-key entry", h.entries)
	}
}
