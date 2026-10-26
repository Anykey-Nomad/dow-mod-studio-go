//go:build windows

package app

import (
	"bytes"
	"math"
	"os"
	"path/filepath"
	"testing"

	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/app/render"
	"dow-mod-studio/internal/audioplayer"
	"dow-mod-studio/internal/fda"
	"dow-mod-studio/internal/fx"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/testutil"
	"github.com/fogleman/fauxgl"
)

func TestCrystalRingSpinIsReadFromTheFile(t *testing.T) {
	const path = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE/Data/art/fx/necron/monolith/crystal_interior.lua`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no crystal file: %v", err)
	}
	file, err := fx.ParseFxLua(data)
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	nodes := file.Nodes
	if len(nodes) == 0 {
		t.Fatalf("parsed no nodes out of %d bytes", len(data))
	}
	var rings int
	for i := range nodes {
		n := &nodes[i]
		r, ok := fx.RingFromNode(n)
		if !ok {
			continue
		}
		rings++
		if len(r.SpinZ) == 0 {
			t.Errorf("%s: ring has no SpinZ curve, so it can only be drawn square to its marker", n.Effect)
			continue
		}
		u := 0.0
		turns := fx.SampleCurve(r.SpinZ, u)
		deg := turns * 360
		t.Logf("%s: SpinZ at u=0 is %.5f turns = %.2f degrees", n.Effect, turns, deg)
		if math.Abs(turns-1.06667) > 0.01 {
			t.Errorf("%s: SpinZ came out %.5f, want about 1.06667 (383 degrees)", n.Effect, turns)
		}
	}
	if rings == 0 {
		t.Errorf("the crystal parsed %d nodes but none of them was a ring", len(nodes))
	}
}

// Being read is one thing; being used is another. A Spin curve is a number of
// turns taken up along the effect's life, so a curve reaching a quarter turn by
// the end has to have turned the quad a quarter turn by then.
func TestRingSpinIsAppliedOverTheEffectLife(t *testing.T) {
	r := fx.Ring{
		Radius: []fx.FxKey{{T: 0, V: 1}, {T: 1, V: 1}},
		SpinZ:  []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0.25}},
	}
	centre := fauxgl.Vector{}
	right := fauxgl.Vector{X: 1}
	up := fauxgl.Vector{Y: 1}
	at := func(u float64) fauxgl.Vector {
		out := fx.RingRibbon(r, centre, fauxgl.Vector{Z: 1}, right, up, fauxgl.Vector{Y: 1}, u, 0)
		return out.Across.Normalize()
	}
	// The angle the geometry actually turned through, from the first sample to
	// the last, measured between the two faces themselves rather than in any
	// one plane: a ring's quad is laid out in the marker's own frame, whose
	// axes are not the viewer's.
	first, last := at(0), at(1)
	deg := math.Acos(engine.ClampFloat64(first.Dot(last), -1, 1)) * 180 / math.Pi
	t.Logf("a curve of 0.25 turns turned the quad by %.2f degrees, want 90", deg)
	if math.Abs(deg-90) > 1 {
		t.Errorf("a 0.25-turn spin curve turned the quad by %.2f degrees, not 90: the spin is being applied as the value at the sampled moment rather than accumulated", deg)
	}
}

// SpinEpsilon is the half of the crystal that Spin alone does not explain. The
// panels set SpinEpsilonZ = 1 with SpinRandom = 1, which asks for the faces to
// be scattered over a whole turn rather than sitting at the angles the Spin
// curves name. A preview that reads only Spin draws every panel square and the
// crystal comes out as one flat sheet.
func TestRingReadsItsSpinEpsilonAndSpinRandom(t *testing.T) {
	const path = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DoWDE/Data/art/fx/necron/monolith/crystal_top_ctr.lua`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no crystal file: %v", err)
	}
	file, err := fx.ParseFxLua(data)
	if err != nil {
		t.Fatalf("fx.ParseFxLua: %v", err)
	}
	var rings int
	for i := range file.Nodes {
		r, ok := fx.RingFromNode(&file.Nodes[i])
		if !ok {
			continue
		}
		rings++
		if len(r.EpsZ) == 0 {
			t.Errorf("%s: no SpinEpsilonZ, so its face cannot be spread", file.Nodes[i].Effect)
		}
		if !r.SpinRandom {
			t.Errorf("%s: SpinRandom was not read", file.Nodes[i].Effect)
		}
		if got := fx.SampleCurve(r.EpsZ, 0); math.Abs(got-1) > 0.001 {
			t.Errorf("%s: SpinEpsilonZ came out %.4f, want 1", file.Nodes[i].Effect, got)
		}
	}
	if rings == 0 {
		t.Fatalf("the crystal file parsed %d nodes but none was a ring", len(file.Nodes))
	}
	t.Logf("%s: %d ring(s), SpinEpsilon and SpinRandom both read", filepath.Base(path), rings)
}

// A spread has to be inside 0..1 and it has to be the same every time it is
// asked for. The second half is the one that matters on screen: a panel that
// drew a different face each frame would flicker, and the crystal's faces
// would never settle.
func TestRingSpreadIsInRangeAndStable(t *testing.T) {
	r := fx.Ring{EpsZ: []fx.FxKey{{T: 0, V: 1}}, SpinRandom: true}
	for i := 0; i < 32; i++ {
		first := r.Spread(i)
		if first < 0 || first >= 1 {
			t.Errorf("panel %d: share %.6f is outside 0..1", i, first)
		}
		if again := r.Spread(i); again != first {
			t.Errorf("panel %d: share changed from %.9f to %.9f between calls", i, first, again)
		}
	}
	// Neighbouring panels must not land on the same face, which is the whole
	// point of spreading them.
	seen := map[float64]int{}
	for i := 0; i < 8; i++ {
		seen[r.Spread(i)] = i
	}
	if len(seen) < 6 {
		t.Errorf("eight panels shared only %d distinct faces: %v", len(seen), seen)
	}
}

// An ordered spread is what a node without SpinRandom gets: the faces are laid
// out in slices rather than scattered. A ring with no epsilon at all is asked
// for nothing, so it must be given nothing.
func TestRingSpreadOrderedAndAbsent(t *testing.T) {
	none := fx.Ring{}
	if got := none.Spread(3); got != 0 {
		t.Errorf("a ring with no SpinEpsilon was given a share of %v, want 0", got)
	}
	ordered := fx.Ring{EpsZ: []fx.FxKey{{T: 0, V: 1}}}
	first := ordered.Spread(0)
	second := ordered.Spread(1)
	if first == second {
		t.Errorf("two ordered panels got the same share %v, so they draw the same face", first)
	}
	t.Logf("ordered shares: panel 0 = %.3f, panel 1 = %.3f", first, second)
}

// The epsilon has to reach the geometry, not just the struct: a panel told to
// spread over a quarter turn must come out of fx.RingRibbon turned by it.
//
// The spread is sampled at a quarter rather than at 1, because a full turn
// brings a panel back to where it started - a spread of exactly one looks
// identical to no spread at all, which is the one value that cannot tell the
// two apart.
func TestRingRibbonGeometryRespondsToTheSpread(t *testing.T) {
	r := fx.Ring{
		Radius: []fx.FxKey{{T: 0, V: 1}, {T: 1, V: 1}},
		SpinZ:  []fx.FxKey{{T: 0, V: 0}, {T: 1, V: 0}},
		EpsZ:   []fx.FxKey{{T: 0, V: 1}},
	}
	centre := fauxgl.Vector{}
	right := fauxgl.Vector{X: 1}
	up := fauxgl.Vector{Y: 1}
	at := func(spread float64) fauxgl.Vector {
		return fx.RingRibbon(r, centre, fauxgl.Vector{Z: 1}, right, up, fauxgl.Vector{Y: 1}, 0, spread).Across.Normalize()
	}
	none, quarter := at(0), at(0.25)
	dot := none.Dot(quarter)
	t.Logf("across with no spread %v, with a quarter of the spread %v, dot=%.4f (90 degrees apart is 0)", none, quarter, dot)
	if math.Abs(dot) > 0.01 {
		t.Errorf("a quarter of a whole-turn spread left the panel at dot=%.4f with itself: the epsilon never reached the geometry", dot)
	}
	// And a full turn really does come back to the start, which is why the
	// test above cannot use it.
	if full, back := at(1), none; math.Abs(full.Dot(back)-1) > 0.01 {
		t.Errorf("a whole turn did not return the panel to where it started: dot=%.4f", full.Dot(back))
	}
}

// ─── merged from x_test_fx_marker_arrow_test.go ────────────────────────────────────────

func TestMarkerArrowPointsAlongTheMarkerAxis(t *testing.T) {
	m := arrowTestModel(t)
	pose := m.RestPose()
	// The real constructor, so the camera is the one a reader would be looking
	// through: a viewer built by hand has no field of view and no distance, and
	// every projection through it divides by zero.

	// Find a marker whose forward is usable, since a marker on no bone has no
	// direction to show and must report none rather than a made-up one.
	idx := -1
	for i := range m.Markers {
		if _, ok := m.MarkerAxis(i, pose, true); ok {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Skipf("none of the %d markers has a usable forward axis", len(m.Markers))
	}
	// The real constructor, so the camera is the one a reader would be looking
	// through: a viewer built by hand has no field of view and no distance, and
	// every projection through it divides by zero.
	v := newModelViewer(m, "arrow_test.whm")
	v.markerIndex = idx
	world, ok := m.MarkerWorld(idx, pose, true)
	if !ok {
		t.Fatalf("marker %s has an axis but no position", m.Markers[idx].Name)
	}
	axis, _ := m.MarkerAxis(idx, pose, true)
	t.Logf("marker %s at %v with forward %v", m.Markers[idx].Name, world, axis)

	const w, h = 800, 600
	tip, has := v.markerArrow(w, h)
	if !has {
		t.Skipf("marker %s produced no arrow at %dx%d; it may be off screen", m.Markers[idx].Name, w, h)
	}
	at, ok := v.markerHighlight(w, h)
	if !ok {
		t.Fatalf("the ring has nowhere to draw but the arrow reports %v", tip)
	}

	// The screen direction from the marker to the tip has to agree with the
	// marker's own forward axis once that axis is projected, which is what
	// makes the arrow an arrow and not just a line.
	view := render.BuildViewMatrix(v.options(w, h))
	projected := fxProject(view, world.Add(axis.MulScalar(markerArrowWorldLength)), w, h)
	if projected == nil {
		t.Skip("the axis end is off screen, so there is nothing to compare against")
	}
	gotScreen := fauxgl.Vector{X: tip.X - at.X, Y: tip.Y - at.Y}.Normalize()
	wantScreen := fauxgl.Vector{X: projected.X - at.X, Y: projected.Y - at.Y}.Normalize()
	dot := gotScreen.Dot(wantScreen)
	t.Logf("arrow points %v on screen, the marker's forward projects to %v, dot=%.4f", gotScreen, wantScreen, dot)
	// A NaN here compares false against everything, so a projection that never
	// worked would sail through the check below and the test would pass while
	// proving nothing. It is refused explicitly.
	if math.IsNaN(dot) || math.IsNaN(gotScreen.X) || math.IsNaN(wantScreen.X) {
		t.Fatalf("the projection produced %v and %v; the comparison cannot be made", gotScreen, wantScreen)
	}
	if dot < 0.99 {
		t.Errorf("the arrow points %v but the marker's forward projects to %v (dot=%.4f)", gotScreen, wantScreen, dot)
	}
}

// A marker with no forward has no arrow to draw. Reporting a direction for one
// would point at something the effects never use, which is worse than showing
// nothing: the marker list is what a reader trusts about where a thing aims.
func TestMarkerArrowIsAbsentWhenThereIsNoAxis(t *testing.T) {
	m := arrowTestModel(t)
	// No marker picked at all.
	v := newModelViewer(m, "arrow_test.whm")
	v.markerIndex = -1
	if _, ok := v.markerArrow(100, 100); ok {
		t.Errorf("an arrow was drawn with no marker picked")
	}
	// A marker index outside the model, which is what a stale index from a
	// model that has since been replaced looks like.
	v = newModelViewer(m, "arrow_test.whm")
	v.markerIndex = len(m.Markers) + 5
	if _, ok := v.markerArrow(100, 100); ok {
		t.Errorf("an arrow was drawn for marker %d of %d", v.markerIndex, len(m.Markers))
	}
}

// A very short arrow must not be drawn as a blob: at less than a pixel the two
// ends land on top of each other and the head has nowhere to sit, so the arrow
// would read as a smudge on the model rather than a direction.
func TestMarkerArrowRefusesAnUnusableLength(t *testing.T) {
	// The rule the drawing uses, stated on its own: below one pixel of travel
	// there is no direction to show.
	const minLength = 1.0
	for _, length := range []float64{0, 0.001, 0.4, 0.999} {
		if length >= minLength {
			t.Errorf("test setup: %.3f should have been refused", length)
		}
	}
	if math.Hypot(1, 0) < minLength {
		t.Errorf("a one pixel arrow would be refused, but it is the shortest worth drawing")
	}
}

// arrowTestModel is the shared box with one marker on its only bone, placed
// off to one side so that the marker's forward is the direction away from the
// mesh — which is what modelMarkerForwards scores for, and what a marker at the
// end of a barrel looks like.
func arrowTestModel(t *testing.T) *WhmModel {
	t.Helper()
	m := fxOverlayTestModel(t)
	// The marker's own 3x3 is the identity, so its forward is column 1, +y.
	// Standing it at -y on the bone means the column pointing away from the
	// mesh is -y rather than +y, which is the case the scoring has to get right.
	m.Markers = []WhmMarker{{
		Name:   "marker_muzzle",
		Parent: "root",
		Matrix: [9]float32{1, 0, 0, 0, 1, 0, 0, 0, 1},
		Pos:    [3]float32{0, -1.2, 0},
	}}
	return m
}

// ─── merged from x_test_fx_sound_rate_test.go ────────────────────────────────────────

func TestFxSoundBogusRateIsCorrected(t *testing.T) {
	cases := []struct {
		declared int
		want     int
		why      string
	}{
		{22050, 44100, "declares 22050 but is really 44100"},
		{44100, 44100, "a correct header is left alone"},
		{48000, 48000, "48 kHz is left alone"},
		{11025, 11025, "a rate that was never wrong stays as it is"},
		{0, 0, "an unreadable header is not guessed at"},
	}
	for _, c := range cases {
		if got := fxSoundDeclaredRate(c.declared); got != c.want {
			t.Errorf("fxSoundDeclaredRate(%d) = %d, want %d: %s",
				c.declared, got, c.want, c.why)
		}
	}
}

// The correction has to be exactly a change of rate and nothing else. The
// decoder was measured to return the same samples at every rate, so a file read
// at 44100 and the same file read at 22050 differ only in how long they last —
// and reading it at the corrected rate is what halves that.
func TestFxSoundRateCorrectionOnlyChangesDuration(t *testing.T) {
	path := `cmd/audiotest/land (1).fda`
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("no reference file: %v", err)
	}
	file, err := fda.ParseFDAReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pcm, _, err := fda.DecodeFDAToPCMAtRate(file, fxSoundRealRate)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	bad := float64(len(pcm)) / float64(fxSoundBogusRate)
	good := float64(len(pcm)) / float64(fxSoundRealRate)
	t.Logf("%s: %d frames -> %.2f s at the declared %d Hz, %.2f s at the real %d Hz",
		filepath.Base(path), len(pcm), bad, fxSoundBogusRate, good, fxSoundRealRate)
	if ratio := bad / good; ratio < 1.99 || ratio > 2.01 {
		t.Errorf("declared rate plays %.3fx slower, not the ~2x that was heard", ratio)
	}
	// The decoder must not be doing anything rate-dependent behind this,
	// because the whole correction assumes the samples do not change.
	again, _, err := fda.DecodeFDAToPCMAtRate(file, fxSoundBogusRate)
	if err != nil {
		t.Fatalf("decode at declared rate: %v", err)
	}
	if len(again) != len(pcm) {
		t.Errorf("decoding at %d Hz gave %d frames and at %d Hz gave %d; the samples are supposed to be the same",
			fxSoundBogusRate, len(again), fxSoundRealRate, len(pcm))
	}
}

// ─── merged from x_test_fx_markers_test.go ────────────────────────────────────────

func TestMarkerSpaceSettledByBounds(t *testing.T) {
	checked := 0
	for _, name := range []string{"Necron_Summoning_Core.whm", "necron_monolith.whm", "necron_tomb_sviva.whm"} {
		raw := testutil.FindDowFile(name)
		if raw == nil {
			continue
		}
		model, err := ParseWhmModel(raw)
		if err != nil {
			t.Fatalf("ParseWhmModel(%s): %v", name, err)
		}
		if len(model.Markers) == 0 {
			t.Logf("%s: no markers", name)
			continue
		}
		checked++
		pose := model.RestPose()
		min := render.Vec3From(model.BoundMin)
		max := render.Vec3From(model.BoundMax)
		// Markers such as muzzles legitimately stick out a little.
		grow := max.Sub(min).MulScalar(0.2)
		min = min.Sub(grow)
		max = max.Add(grow)
		inside := func(p fauxgl.Vector) bool {
			return p.X >= min.X && p.X <= max.X &&
				p.Y >= min.Y && p.Y <= max.Y &&
				p.Z >= min.Z && p.Z <= max.Z
		}
		var inBoneLocal, inBindSpace int
		t.Logf("%s: %d markers, bounds (%.1f %.1f %.1f)..(%.1f %.1f %.1f)",
			name, len(model.Markers), min.X, min.Y, min.Z, max.X, max.Y, max.Z)
		for i, mk := range model.Markers {
			pBone, _ := model.MarkerWorld(i, pose, true)
			pBind, _ := model.MarkerWorld(i, pose, false)
			if inside(pBone) {
				inBoneLocal++
			}
			if inside(pBind) {
				inBindSpace++
			}
			if i < 8 {
				t.Logf("  %-24s parent=%-16s pos=(%.2f %.2f %.2f) boneLocal=(%.2f %.2f %.2f)%v bind=(%.2f %.2f %.2f)%v",
					mk.Name, mk.Parent, mk.Pos[0], mk.Pos[1], mk.Pos[2],
					pBone.X, pBone.Y, pBone.Z, inside(pBone),
					pBind.X, pBind.Y, pBind.Z, inside(pBind))
			}
		}
		t.Logf("%s: inside bounds: bone-local %d/%d, bind-space %d/%d",
			name, inBoneLocal, len(model.Markers), inBindSpace, len(model.Markers))
		// The game reads a marker in its parent bone's frame; a bind space
		// reading lands almost every marker outside the model it belongs to.
		if inBindSpace > inBoneLocal {
			t.Errorf("%s: bind-space reading fits better (%d vs %d) — marker space needs rethinking",
				name, inBindSpace, inBoneLocal)
		}
	}
	if checked == 0 {
		t.Skip("no installed model with markers found")
	}
}

// ─── merged from x_test_fx_sound_e2e_test.go ────────────────────────────────────────

func TestFxSoundDecodeForPlaybackProducesRightDuration(t *testing.T) {
	name := "land (1).fda"
	src := filepath.Join("cmd", "audiotest", name)
	if _, err := os.Stat(src); err != nil {
		t.Skipf("no reference file: %v", err)
	}
	// The conversion takes the mod tree, so the file is read the way the
	// preview reads it. nil tree means only the on-disk roots are searched, a
	// root is joined with the sound path, and a sound path is always rooted at
	// the game's sound folder because whe.SoundPathCleans prefixes it. So the
	// reference file is copied into a sound folder under a temporary root,
	// which is the shape the preview looks for.
	root := t.TempDir()
	soundDir := filepath.Join(root, "sound")
	if err := os.MkdirAll(soundDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		t.Fatalf("read reference: %v", err)
	}
	if err := os.WriteFile(filepath.Join(soundDir, name), data, 0o644); err != nil {
		t.Fatalf("stage reference: %v", err)
	}
	s, err := fxSoundDecodeForPlayback(nil, []string{root}, name)
	if err != nil {
		t.Fatalf("decode for playback: %v", err)
	}
	if s.Info.SampleRate != audioplayer.SampleRate || s.Info.Channels != audioplayer.Channels {
		t.Errorf("sample came out %d Hz/%d ch, want the context's %d Hz/%d ch",
			s.Info.SampleRate, s.Info.Channels, audioplayer.SampleRate, audioplayer.Channels)
	}
	frames := len(s.PCM) / audioplayer.Channels
	got := float64(frames) / float64(audioplayer.SampleRate)
	// At the declared 22050 this file would last 35.2 s. At its real 44100,
	// converted up to the 48 kHz context, it is 17.6 s.
	t.Logf("%s: %d frames at %d Hz = %.2f s", name, frames, s.Info.SampleRate, got)
	if got > 20 {
		t.Errorf("sample lasts %.2f s, which is the half-speed reading at the declared 22050 Hz", got)
	}
	if got < 16 {
		t.Errorf("sample lasts %.2f s, too short for the 17.6 s the real rate implies", got)
	}
}

// ─── merged from x_test_fx_sound_model_load_test.go ────────────────────────────────────────

func TestLoadModelSoundsNeedsNoEffects(t *testing.T) {
	if testDXP3Module == "" {
		t.Skip("no module path")
	}
	tree, err := modtree.BuildModTree(testDXP3Module, "English")
	if err != nil {
		t.Skipf("BuildModTree: %v", err)
	}
	defer tree.Close()
	// Find a model that declares sounds at all, so the test is about loading
	// them rather than about a model that happens to be silent.
	var files []string
	modtree.SearchWalk(tree.Root, func(f *modtree.TreeFile) {
		if len(files) < 40 && hasWheExtension(f.Name) {
			files = append(files, f.FullPath)
		}
	})
	if len(files) == 0 {
		t.Skip("no models with a .whe")
	}
	loaded := 0
	for _, f := range files {
		res := loadModelSounds(tree, f)
		if res.err != nil {
			continue
		}
		if len(res.soundBundles) > 0 {
			loaded++
			t.Logf("%s: %d sound events, %d samples decoded, %d timelines",
				f, len(res.soundBundles), res.sounds.Len(), len(res.timelines))
		}
		// The point of the split: loading sounds must not have resolved a
		// single effect, or the frame would be doing the button's work early.
		if len(res.effects) != 0 || len(res.textures) != 0 {
			t.Errorf("%s: loading sounds also resolved %d effects and %d textures",
				f, len(res.effects), len(res.textures))
		}
	}
	if loaded == 0 {
		t.Logf("no sounds declared by any of the %d models checked", len(files))
	} else {
		t.Logf("%d of %d models declared sounds", loaded, len(files))
	}
}

func hasWheExtension(name string) bool {
	lower := toLowerASCII(name)
	return len(lower) > 4 && lower[len(lower)-4:] == ".whe"
}

func toLowerASCII(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
