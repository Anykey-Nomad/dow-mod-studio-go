package render

import (
	"fmt"
	"image"
	"os"
	"strings"

	"dow-mod-studio/internal/whm"
	"gioui.org/layout"
)

// These types are the seam between the 3D view and its Vulkan renderer, so the
// view stays portable and can fall back to software when Vulkan is unavailable.

// Handle renders models on the GPU. A nil handle means the window's
// backend cannot do it, and every frame goes through the software renderer
// instead.
type Handle interface {
	// Render draws one frame and returns it ready to paint.
	Render(
		model *whm.Model,
		pose WhmPose,
		opts RenderOptions,
		materials map[string]WhmMaterial,
		rsh func(material string) ([]byte, bool),
	) (Frame, error)
	// Forget drops the cached geometry of a model whose tab is closing, so a
	// long session does not hold every model it ever opened.
	Forget(model *whm.Model)
	// GeometryChanged invalidates a model cache after its vertices, indices,
	// or mesh list are changed in place.
	GeometryChanged(model *whm.Model)
	// Release frees the device resources the handle owns.
	Release()
}

// Frame is a finished frame that lives in video memory, so painting it
// costs a blit and nothing else.
type Frame interface {
	// Paint adds the frame to the op stream, scaled to fit rect and centred
	// inside it.
	Paint(gtx layout.Context, rect image.Rectangle)
	// Overlay runs fn with the frame's own transform in place, so something
	// drawn in the frame's pixels lands on the model exactly where the frame
	// put it however the canvas is scaled. It is what lets the marker ring
	// and its arrow be drawn over a hardware frame as well as a software
	// one, rather than being reachable on only one of the two paths.
	Overlay(gtx layout.Context, rect image.Rectangle, fn func(layout.Context))
	// Release drops the frame. Frames are owned by the renderer, which pools
	// them, so this is a no-op and only exists for symmetry.
	Release()
}

// ─── GPU API selection (merged from gpu_api.go) ─────────────────────────────

// DefaultGPUAPI is the backend the program initializes Gio with.
const DefaultGPUAPI = "vulkan"

// NormalizeGPUAPI maps a configured value onto the backend actually used.
func NormalizeGPUAPI(api string) string {
	if strings.EqualFold(strings.TrimSpace(api), DefaultGPUAPI) {
		return DefaultGPUAPI
	}
	// Legacy configs and unknown values must not let Gio fall through to EGL.
	return DefaultGPUAPI
}

// SetGPUAPI forces Gio to initialize its Vulkan driver before the first
// window frame. Without this, Gio falls through to EGL on Windows.
func SetGPUAPI(api string) error {
	api = NormalizeGPUAPI(api)
	if err := os.Setenv("DOW_GPU_API", api); err != nil {
		return fmt.Errorf("select %s GPU backend: %w", api, err)
	}
	return nil
}
