package app

import (
	"dow-mod-studio/internal/app/render"
	"encoding/json"
	"os"
	"testing"
)

func TestGPUAPIDefaultsToVulkanAndOverridesEnvironment(t *testing.T) {
	t.Setenv("DOW_GPU_API", "egl")
	if err := render.SetGPUAPI(""); err != nil {
		t.Fatalf("render.SetGPUAPI: %v", err)
	}
	if got := os.Getenv("DOW_GPU_API"); got != render.DefaultGPUAPI {
		t.Fatalf("DOW_GPU_API = %q, want %q", got, render.DefaultGPUAPI)
	}
}

func TestPersistedGPUAPIDefaultsToVulkan(t *testing.T) {
	var old persistedState
	if err := json.Unmarshal([]byte(`{"version":1,"darkTheme":true}`), &old); err != nil {
		t.Fatalf("unmarshal legacy state: %v", err)
	}
	ui := newLocaleTestUI()
	ui.applyPersistedSettings(old)
	if got := ui.gpuAPI; got != render.DefaultGPUAPI {
		t.Fatalf("legacy config selected GPU API %q, want %q", got, render.DefaultGPUAPI)
	}

	configDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", configDir)
	t.Setenv("AppData", configDir)
	t.Setenv("LOCALAPPDATA", configDir)
	if err := savePersistedState(ui); err != nil {
		t.Fatalf("save state: %v", err)
	}
	back, _, err := loadPersistedState()
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if got := back.GPUAPI; got != render.DefaultGPUAPI {
		t.Fatalf("saved gpuAPI = %q, want %q", got, render.DefaultGPUAPI)
	}
}
