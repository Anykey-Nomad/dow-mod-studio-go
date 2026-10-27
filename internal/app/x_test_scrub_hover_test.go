package app

import (
	"image"
	"testing"

	"dow-mod-studio/internal/testutil"
	"gioui.org/f32"
	"gioui.org/io/event"
	"gioui.org/io/input"
	"gioui.org/io/pointer"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
)

// The frame strip's readout must not cost the canvas its pointer.
//
// The strip's readout follows the cursor, and the obvious way to feed it — a
// pointer area of the strip's own, registered by the overlay — is exactly what
// broke seeking: an area registered after the canvas's clip has been popped
// starts a chain of its own in Gio's hit tree, and while the pointer is inside
// its rectangle nothing below that chain is reached. The rectangle is the
// strip, so the canvas, which is what seeks, went deaf precisely where the
// strip is — presses, drags and releases alike — while the rest of the canvas
// went on working.
//
// This runs a canvas area registered the way ModelCanvas registers one,
// optionally with the real ModelAnimScrubOverlay on top of it, inside an
// isolated router, and reports what each tag receives. The pointer is driven
// the way the window drives it: a queue, then a frame that delivers, then a
// frame that reads.
type scrubProbe struct {
	ui            *uiState
	v             *modelViewer
	tab           *fileTab
	ops           op.Ops
	r             input.Router
	gtx           layout.Context
	press, drag   int
	release, move int
	draw          func()
}

func newScrubProbe(withOverlay bool) *scrubProbe {
	p := &scrubProbe{}
	p.ui = newUI(testutil.UnitTestTheme())
	p.v = testKeyEditViewer(8)
	p.tab = &fileTab{Model: p.v}
	p.gtx = layout.Context{Ops: &p.ops, Source: p.r.Source(), Constraints: layout.Exact(image.Pt(1200, 700))}

	draw := func() {
		c := clip.Rect{Max: image.Pt(1200, 700)}.Push(p.gtx.Ops)
		event.Op(p.gtx.Ops, &p.v.Tag)
		for {
			ev, ok := p.gtx.Event(pointer.Filter{
				Target:  &p.v.Tag,
				Kinds:   pointer.Press | pointer.Drag | pointer.Release | pointer.Cancel | pointer.Scroll | pointer.Move | pointer.Enter | pointer.Leave,
				ScrollX: pointer.ScrollRange{Min: -1 << 60, Max: 1 << 60},
				ScrollY: pointer.ScrollRange{Min: -1 << 60, Max: 1 << 60},
			})
			if !ok {
				break
			}
			e, ok := ev.(pointer.Event)
			if !ok {
				continue
			}
			switch e.Kind {
			case pointer.Press:
				p.press++
			case pointer.Drag:
				p.drag++
			case pointer.Release:
				p.release++
			case pointer.Move:
				p.move++
			}
			if e.Kind == pointer.Move || e.Kind == pointer.Enter || e.Kind == pointer.Leave {
				// what ModelCanvas does with the same events
				UpdateScrubHover(p.v, e)
			}
		}
		c.Pop()
		if withOverlay {
			ModelAnimScrubOverlay(p.ui, p.gtx, p.tab)
		}
		p.r.Frame(&p.ops)
	}
	p.draw = draw
	draw()
	return p
}

func (p *scrubProbe) frame(evs ...pointer.Event) {
	for _, e := range evs {
		p.r.Queue(e)
	}
	// one frame to deliver, one to read what was delivered
	p.draw()
	p.draw()
}

func TestTheStripReadoutLeavesTheCanvasItsPointer(t *testing.T) {
	for _, tc := range []struct {
		name        string
		withOverlay bool
	}{
		{"canvas alone", false},
		{"canvas with the scrub overlay on top", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newScrubProbe(tc.withOverlay)
			if tc.withOverlay && p.v.ScrubRect.Empty() {
				t.Fatalf("no strip: ScrubRect=%v", p.v.ScrubRect)
			}
			mid := f32.Pt(600, 677)
			if tc.withOverlay {
				r := p.v.ScrubRect
				mid = f32.Pt(float32(r.Min.X+r.Max.X)/2, float32(r.Min.Y+r.Max.Y)/2)
				t.Logf("strip rect %v, pressing at %v", r, mid)
			}

			p.frame(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: mid})
			t.Logf("after move: canvas move=%d press=%d hover=%v pos=(%.1f, %.1f)",
				p.move, p.press, p.v.ScrubHover, p.v.ScrubHoverX, p.v.ScrubHoverY)
			if tc.withOverlay {
				if !p.v.ScrubHover {
					t.Errorf("a move over the strip did not put the readout under the cursor")
				}
				// The readout is drawn at these coordinates in canvas space, and
				// a position delivered to an area is already in that space: the
				// strip's own corner added to it put the box below the canvas,
				// where the canvas's clip hid it.
				if p.v.ScrubHoverX != mid.X || p.v.ScrubHoverY != mid.Y {
					t.Errorf("the readout sits at (%.1f, %.1f), want the cursor's (%.1f, %.1f)",
						p.v.ScrubHoverX, p.v.ScrubHoverY, mid.X, mid.Y)
				}
			}

			p.frame(pointer.Event{Kind: pointer.Press, Source: pointer.Mouse, Position: mid, Buttons: pointer.ButtonPrimary})
			t.Logf("after press: canvas press=%d drag=%d seeking=%v time=%.4f",
				p.press, p.drag, p.v.Seeking, p.v.Time)

			// The window never queues a Drag: a move while a button is held
			// becomes one inside the router.
			dragAt := f32.Pt(mid.X+30, mid.Y)
			p.frame(pointer.Event{Kind: pointer.Move, Source: pointer.Mouse, Position: dragAt, Buttons: pointer.ButtonPrimary})
			t.Logf("after drag: canvas drag=%d time=%.4f seeking=%v", p.drag, p.v.Time, p.v.Seeking)

			p.frame(pointer.Event{Kind: pointer.Release, Source: pointer.Mouse, Position: dragAt})
			t.Logf("after release: canvas release=%d", p.release)

			if p.press == 0 {
				t.Errorf("the canvas never got the press")
			}
			if p.drag == 0 {
				t.Errorf("the canvas never got the drag")
			}
			if p.release == 0 {
				t.Errorf("the canvas never got the release")
			}
		})
	}
}
