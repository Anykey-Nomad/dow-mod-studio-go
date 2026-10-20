package main

import (
	"fmt"
	"os"
	"strings"
)

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
