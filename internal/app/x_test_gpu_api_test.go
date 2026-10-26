package app

import (
	"encoding/json"
	"os"
	"testing"
)

func TestGPUAPIDefaultsToVulkanAndOverridesEnvironment(t *testing.T) {
	t.Setenv("DOW_GPU_API", "egl")
	if err := setGPUAPI(""); err != nil {
		t.Fatalf("setGPUAPI: %v", err)
	}
	if got := os.Getenv("DOW_GPU_API"); got != defaultGPUAPI {
		t.Fatalf("DOW_GPU_API = %q, want %q", got, defaultGPUAPI)
	}
}

func TestPersistedGPUAPIDefaultsToVulkan(t *testing.T) {
	var old persistedState
	if err := json.Unmarshal([]byte(`{"version":1,"darkTheme":true}`), &old); err != nil {
		t.Fatalf("unmarshal legacy state: %v", err)
	}
	ui := newLocaleTestUI()
	ui.applyPersistedSettings(old)
	if got := ui.gpuAPI; got != defaultGPUAPI {
		t.Fatalf("legacy config selected GPU API %q, want %q", got, defaultGPUAPI)
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
	if got := back.GPUAPI; got != defaultGPUAPI {
		t.Fatalf("saved gpuAPI = %q, want %q", got, defaultGPUAPI)
	}
}
