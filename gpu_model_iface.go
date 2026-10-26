package main

import (
	"fmt"
	"image"
	"os"
	"strings"

	"gioui.org/layout"
)

// These types are the seam between the 3D view and its Vulkan renderer, so the
// view stays portable and can fall back to software when Vulkan is unavailable.

// modelGpuHandle renders models on the GPU. A nil handle means the window's
// backend cannot do it, and every frame goes through the software renderer
// instead.
type modelGpuHandle interface {
	// Render draws one frame and returns it ready to paint.
	Render(
		model *WhmModel,
		pose WhmPose,
		opts RenderOptions,
		materials map[string]WhmMaterial,
		rsh func(material string) ([]byte, bool),
	) (modelGpuFrame, error)
	// forget drops the cached geometry of a model whose tab is closing, so a
	// long session does not hold every model it ever opened.
	forget(model *WhmModel)
	// geometryChanged invalidates a model cache after its vertices, indices,
	// or mesh list are changed in place.
	geometryChanged(model *WhmModel)
	// release frees the device resources the handle owns.
	release()
}

// modelGpuFrame is a finished frame that lives in video memory, so painting it
// costs a blit and nothing else.
type modelGpuFrame interface {
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

// modelGpu opens the hardware renderer on first use and returns it, or nil when
// there is none. Gio creates the window's GPU along with the first frame, so
// this cannot be done any earlier.
func (ui *uiState) modelGpuRenderer() modelGpuHandle {
	if ui.modelGpuTried {
		return ui.modelGpu
	}
	ui.modelGpuTried = true
	ui.modelGpu = newModelGpuForWindow(ui.win)
	// A tab can be laid out before the window has drawn its first frame, and
	// Gio only creates the device then. That is not a verdict on the backend,
	// so the next layout asks again instead of falling back for good.
	if ui.modelGpu == nil && modelGpuTooEarly {
		ui.modelGpuTried = false
	}
	return ui.modelGpu
}

func (ui *uiState) geometryChangedModelGpu(model *WhmModel) {
	if model == nil || ui.modelGpu == nil {
		return
	}
	ui.modelGpu.geometryChanged(model)
}

func (ui *uiState) forgetModelGpu(model *WhmModel) {
	if model == nil || ui.modelGpu == nil {
		return
	}
	ui.modelGpu.forget(model)
}

// releaseModelGpu frees the hardware renderer and everything it cached.
func (ui *uiState) releaseModelGpu() {
	for _, t := range ui.openTabs {
		if t.model != nil {
			t.model.gpuFrame = nil
		}
	}
	// The object window's viewport has a viewer of its own, which is not in
	// openTabs, so its video memory frame has to go back separately.
	if ui.unitModel != nil && ui.unitModel.model != nil {
		ui.unitModel.model.gpuFrame = nil
	}
	if ui.modelGpu != nil {
		ui.modelGpu.release()
		ui.modelGpu = nil
	}
	ui.modelGpuTried = false
}

// ─── merged from gpu_api.go ───────────────────────────────────────────────

const defaultGPUAPI = "vulkan"

func normalizeGPUAPI(api string) string {
	if strings.EqualFold(strings.TrimSpace(api), defaultGPUAPI) {
		return defaultGPUAPI
	}
	// Legacy configs and unknown values must not let Gio fall through to EGL.
	return defaultGPUAPI
}

// setGPUAPI forces Gio to initialize its Vulkan driver before the first
// window frame. Without this, Gio falls through to EGL on Windows.
func setGPUAPI(api string) error {
	api = normalizeGPUAPI(api)
	if err := os.Setenv("DOW_GPU_API", api); err != nil {
		return fmt.Errorf("select %s GPU backend: %w", api, err)
	}
	return nil
}
