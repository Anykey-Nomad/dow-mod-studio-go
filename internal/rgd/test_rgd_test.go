package rgd

import "testing"

func TestRgdDictionary(t *testing.T) {
	d := InitDict()
	t.Logf("dict size %d", len(d))
	// check a known hash: "ability_ext" hash?
	h := Hash("ability_ext")
	if name, ok := d[h]; ok {
		t.Logf("ability_ext hash %08x -> %q", h, name)
	} else {
		t.Logf("ability_ext hash %08x not in dict, fallback %q", h, resolveHashName(h, d))
	}
	// check $REF
	h2 := uint32(0x49D60FAE)
	t.Logf("$REF dict -> %q", resolveHashName(h2, d))
	// check health_ext
	h3 := Hash("health_ext")
	t.Logf("health_ext %08x -> %q", h3, resolveHashName(h3, d))
}
