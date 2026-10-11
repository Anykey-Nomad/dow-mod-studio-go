package main

import (
	"dow-mod-studio/internal/fx"
	"testing"

	"github.com/fogleman/fauxgl"
)

// A quarter of the shipped effect files are a single node with no effect =
// of its own: a bare `fx = { properties = { … } }` with the style beside it.
// The parser keys its nodes on effect =, so before these were read it threw
// the whole file away — which took out ah_lightning_beam, and with it the
// lightning beam of every ork that fires one.
//
// These are the shape of that file in miniature, so the behaviour is pinned
// without needing the game installed. The text below is the real
// ah_lightning_beam with the entries thinned out; the property names, types
// and values are its own.
const fxBareBeamLua = `
        fx =
        {
        	properties =
        	{
        		property_00 =
        		{
        			value = 3,
        			type = "VARTYPE_FLOAT",
        			name = "Duration",
        		},
        		property_02 =
        		{
        			value = 2,
        			type = "VARTYPE_INT",
        			name = "Blending",
        		},
        		property_03 =
        		{
        			value = "DATA:ART/FX/AH_LASCANNON_IMAGE.TGA",
        			type = "VARTYPE_STRING",
        			name = "Texture",
        		},
        		property_04 =
        		{
        			value = 1,
        			type = "VARTYPE_BOOL",
        			name = "SelfIlluminated",
        		},
        		property_06 =
        		{
        			value =
        			{
        				0,
        				1,
        				1,
        				1,
        			},
        			type = "VARTYPE_ARRAY_TIMEFLOAT",
        			name = "Length",
        		},
        		property_08 =
        		{
        			value =
        			{
        				0,
        				2,
        				1,
        				10,
        			},
        			type = "VARTYPE_ARRAY_TIMEFLOAT",
        			name = "Segments",
        		},
        		property_12 =
        		{
        			value =
        			{
        				entry_00 =
        				{
        					0,
        					255,
        					255,
        					255,
        					255,
        				},
        				entry_01 =
        				{
        					0.30746,
        					255,
        					255,
        					255,
        					255,
        				},
        			},
        			type = "VARTYPE_ARRAY_TIMECOLOUR",
        			name = "Colour",
        		},
        	},
        	style = "STYLE_BEAM",
        }
`

func TestParseFxLuaReadsTheBareSingleNodeForm(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxBareBeamLua))
	if err != nil {
		t.Fatalf("the bare single-node form did not parse: %v", err)
	}
	if !f.Single {
		t.Error("the file was not marked as the single-node form")
	}
	if len(f.Nodes) != 1 {
		t.Fatalf("expected the one node the file holds, got %d", len(f.Nodes))
	}
	root := f.Root()
	if root == nil {
		t.Fatal("no root")
	}
	if root.Style != "STYLE_BEAM" {
		t.Errorf("style is %q, want STYLE_BEAM", root.Style)
	}
	// The properties are the point: a node that parses but loses its texture
	// and its length would draw an invisible beam.
	if got := root.String("Texture"); got != "DATA:ART/FX/AH_LASCANNON_IMAGE.TGA" {
		t.Errorf("Texture is %q", got)
	}
	if got := root.Float("Blending", 0); got != 2 {
		t.Errorf("Blending is %v, want 2", got)
	}
	if !root.Bool("SelfIlluminated", false) {
		t.Error("SelfIlluminated was lost")
	}
	length, ok := root.Prop("Length")
	if !ok || len(length.Curve) != 2 {
		t.Fatalf("Length curve did not survive: %+v", length)
	}
	if length.Curve[1].V != 1 {
		t.Errorf("Length ends at %v, want 1", length.Curve[1].V)
	}
	colour, ok := root.Prop("Colour")
	if !ok || len(colour.Colour) != 2 {
		t.Fatalf("Colour ramp did not survive: %+v", colour)
	}
}

// The node has to reach the simulation as a beam. Without this the file parses
// and still draws nothing, which is the failure that went unnoticed: the
// parser returned an error that was logged once per load and read past.
func TestBareSingleNodeFormDrawsABeam(t *testing.T) {
	f, err := fx.ParseFxLua([]byte(fxBareBeamLua))
	if err != nil {
		t.Fatal(err)
	}
	sim := fx.NewFxSim(f)
	if len(sim.Beams(fauxgl.Vector{}, fauxgl.Vector{X: 1}, true)) == 0 {
		t.Fatal("the bare form parsed but produced no beam")
	}
	// It has to span a real distance too, not sit as a stub at the origin.
	from := fauxgl.Vector{}
	to := fauxgl.Vector{X: 4}
	for i, r := range sim.Beams(from, to, true) {
		got := r.To.Sub(r.From).Length()
		if got < 3.99 || got > 4.01 {
			t.Errorf("beam %d spans %.4f across a gap of 4", i, got)
		}
	}
}

// The other writing of a node must keep working: a file with effect = paths
// is a tree, and its root is a wrapper whose children are the real nodes.
func TestParseFxLuaStillReadsTheTreeForm(t *testing.T) {
	const treeForm = `
		fxtypes =
		{
			{
				effect = "root",
				fx = { properties = {} },
				style = "STYLE_COMBO",
			},
			{
				effect = "root>inner",
				fx = { properties = { { name = "Texture", type = "VARTYPE_STRING", value = "a.tga" } } },
				style = "STYLE_RING",
			},
		}
	`
	f, err := fx.ParseFxLua([]byte(treeForm))
	if err != nil {
		t.Fatal(err)
	}
	if f.Single {
		t.Error("a file with effect = paths was marked as the single-node form")
	}
	if len(f.Nodes) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(f.Nodes))
	}
	root := f.Root()
	if root == nil || root.Style != "STYLE_COMBO" {
		t.Fatalf("root is %+v, want the STYLE_COMBO wrapper", root)
	}
	children := f.Children(root.Effect)
	if len(children) != 1 || children[0].Effect != "root>inner" {
		t.Fatalf("children of root are %+v", children)
	}
}

// A file with neither shape is still an error: recognising the bare form must
// not turn every unreadable file into an empty one.
func TestParseFxLuaRejectsWhatIsNeitherForm(t *testing.T) {
	for _, bad := range []string{
		"",
		"this is not an effect file at all",
		"{ style = \"STYLE_BEAM\" }",
	} {
		if _, err := fx.ParseFxLua([]byte(bad)); err == nil {
			t.Errorf("accepted %q, which is neither form", bad)
		}
	}
}
