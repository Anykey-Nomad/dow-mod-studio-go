//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	progID     = "DowModStudio.Module"
	progDesc   = "Dawn of War Module"
	extKey     = `Software\Classes\.module`
	progKey    = `Software\Classes\DowModStudio.Module`
	iconKey    = `Software\Classes\DowModStudio.Module\DefaultIcon`
	commandKey = `Software\Classes\DowModStudio.Module\shell\open\command`
)

// getExePath returns the absolute path of current executable.
func getExePath() string {
	p, err := os.Executable()
	if err != nil {
		return ""
	}
	if abs, err2 := filepath.Abs(p); err2 == nil {
		p = abs
	}
	if eval, err := filepath.EvalSymlinks(p); err == nil {
		if _, err := os.Stat(eval); err == nil {
			p = eval
		}
	}
	return p
}

func notifyShellAssocChanged() {
	// SHCNE_ASSOCCHANGED = 0x08000000, SHCNF_IDLIST = 0x0000
	dll := windows.NewLazySystemDLL("shell32.dll")
	proc := dll.NewProc("SHChangeNotify")
	_, _, _ = proc.Call(uintptr(0x08000000), uintptr(0x0000), 0, 0)
}

// isModuleAssociated checks whether double-click on .module will launch this exe.
// On Win10/11 UserChoice overrides HKCU\Software\Classes\.module, so we check it first.
func isModuleAssociated() (bool, string, error) {
	exe := getExePath()
	if exe == "" {
		return false, "", fmt.Errorf("cannot get exe path")
	}
	lowerExe := strings.ToLower(exe)
	base := filepath.Base(exe)
	appProg := "Applications\\" + base
	appCmdKey := `Software\Classes\Applications\` + base + `\shell\open\command`

	// 1) If UserChoice exists, it determines what double-click does.
	if ku, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\UserChoice`, registry.QUERY_VALUE); err == nil {
		prog, _, _ := ku.GetStringValue("ProgId")
		ku.Close()
		if prog != "" {
			if strings.EqualFold(prog, progID) {
				// UserChoice correctly points to our ProgID — verify command
				if k2, err := registry.OpenKey(registry.CURRENT_USER, commandKey, registry.QUERY_VALUE); err == nil {
					cmd, _, _ := k2.GetStringValue("")
					k2.Close()
					if strings.Contains(strings.ToLower(cmd), lowerExe) {
						return true, cmd, nil
					}
					return false, fmt.Sprintf("UserChoice=%q but command=%q mismatch exe %q", prog, cmd, exe), nil
				}
				return false, prog, nil
			}
			if strings.EqualFold(prog, appProg) {
				// Manual "Open with" that points via Applications\<exe>
				if k2, err := registry.OpenKey(registry.CURRENT_USER, appCmdKey, registry.QUERY_VALUE); err == nil {
					cmd, _, _ := k2.GetStringValue("")
					k2.Close()
					if strings.Contains(strings.ToLower(cmd), lowerExe) {
						return true, cmd, nil
					}
					return false, fmt.Sprintf("UserChoice=%q but Applications command=%q", prog, cmd), nil
				}
				return false, fmt.Sprintf("UserChoice=%q (Applications key missing)", prog), nil
			}
			// UserChoice points elsewhere (e.g. Notepad, module_auto_file, legacy ModStudio.exe) → double-click will NOT use us
			return false, fmt.Sprintf("UserChoice=%q overrides (expected %q)", prog, progID), nil
		}
		// empty ProgId → treat as not associated, fall through to HKCU check
	}

	// 2) No UserChoice or empty → HKCU\Software\Classes\.module decides.
	k, err := registry.OpenKey(registry.CURRENT_USER, extKey, registry.QUERY_VALUE)
	if err != nil {
		return false, "", nil
	}
	prog, _, err := k.GetStringValue("")
	k.Close()
	if err != nil || !strings.EqualFold(prog, progID) {
		return false, prog, nil
	}
	k2, err := registry.OpenKey(registry.CURRENT_USER, commandKey, registry.QUERY_VALUE)
	if err != nil {
		return false, prog, nil
	}
	cmd, _, err := k2.GetStringValue("")
	k2.Close()
	if err != nil {
		return false, prog, nil
	}
	if !strings.Contains(strings.ToLower(cmd), lowerExe) {
		return false, cmd, nil
	}
	return true, cmd, nil
}

// registerModuleAssociation creates per-user file association for .module.
// Uses HKCU\Software\Classes which does NOT require elevation on Windows.
func registerModuleAssociation() error {
	exe := getExePath()
	if exe == "" {
		return fmt.Errorf("cannot determine executable path")
	}
	command := `"` + exe + `" "%1"`
	base := filepath.Base(exe)

	// .module → ProgID
	key, _, err := registry.CreateKey(registry.CURRENT_USER, extKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", extKey, err)
	}
	if err := key.SetStringValue("", progID); err != nil {
		key.Close()
		return err
	}
	key.Close()

	// ProgID description
	key2, _, err := registry.CreateKey(registry.CURRENT_USER, progKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", progKey, err)
	}
	_ = key2.SetStringValue("", progDesc)
	key2.Close()

	// DefaultIcon → "<exe>",0
	keyIcon, _, err := registry.CreateKey(registry.CURRENT_USER, iconKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", iconKey, err)
	}
	_ = keyIcon.SetStringValue("", `"`+exe+`",0`)
	keyIcon.Close()

	// shell\open\command
	keyCmd, _, err := registry.CreateKey(registry.CURRENT_USER, commandKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", commandKey, err)
	}
	if err := keyCmd.SetStringValue("", command); err != nil {
		keyCmd.Close()
		return err
	}
	keyCmd.Close()

	// Friendly verb name
	keyOpen, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\DowModStudio.Module\shell\open`, registry.SET_VALUE)
	if err == nil {
		_ = keyOpen.SetStringValue("", "Open with DoW Mod Studio")
		keyOpen.Close()
	}

	// Make Explorer offer us in Open With and resolve double-click even if UserChoice is absent
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\OpenWithProgids`, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue(progID, "")
		k.Close()
	}
	// Also register via Applications\<exe> — this is what "Open with → Choose app" creates, and some Windows builds prefer it
	appCmdKey := `Software\Classes\Applications\` + base + `\shell\open\command`
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, appCmdKey, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("", command)
		k.Close()
	}
	// Optional friendly name for Applications entry
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue("", progDesc)
		k.Close()
	}

	// Remove UserChoice if it does NOT point to us — otherwise HKCU Classes is ignored on Win10/11
	// We cannot compute the Hash, so we delete the key and let Explorer fall back to .module ProgID.
	// This is the crucial fix: previously we only deleted legacy values, leaving e.g. Notepad++ override active.
	if ku, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\UserChoice`, registry.QUERY_VALUE); err == nil {
		prog, _, _ := ku.GetStringValue("ProgId")
		ku.Close()
		if !strings.EqualFold(prog, progID) {
			// Also keep Applications\<exe> if it points to same exe? But HKCU path is more reliable after move,
			// so we still delete it so double-click goes through HKCU\Software\Classes\.module → ProgID
			// which we just updated to current exe path. Deleting forces Explorer to use new path.
			_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\UserChoice`)
		}
	} else {
		// No UserChoice or cannot open — still try to delete (may have been stale)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\UserChoice`)
	}

	// Notify shell so Explorer picks up immediately without waiting seconds
	notifyShellAssocChanged()

	return nil
}

// unregisterModuleAssociation removes per-user association if it belongs to this app.
// It is careful not to delete .module association that points to another app.
func unregisterModuleAssociation() error {
	base := filepath.Base(getExePath())

	// Only delete .module if it points to us
	if k, err := registry.OpenKey(registry.CURRENT_USER, extKey, registry.QUERY_VALUE); err == nil {
		prog, _, _ := k.GetStringValue("")
		k.Close()
		if strings.EqualFold(prog, progID) {
			_ = registry.DeleteKey(registry.CURRENT_USER, extKey)
		}
	}
	// Delete ProgID tree bottom-up (DeleteKey fails if subkeys remain)
	_ = registry.DeleteKey(registry.CURRENT_USER, commandKey)
	_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\DowModStudio.Module\shell\open`)
	_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\DowModStudio.Module\shell`)
	_ = registry.DeleteKey(registry.CURRENT_USER, iconKey)
	_ = registry.DeleteKey(registry.CURRENT_USER, progKey)
	if base != "" {
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base+`\shell\open\command`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base+`\shell\open`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base+`\shell`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base)
	}

	// Remove from OpenWithProgids
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\OpenWithProgids`, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(progID)
		k.Close()
	}
	// Clean UserChoice if it points to us (or to our Applications entry)
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\UserChoice`, registry.QUERY_VALUE); err == nil {
		prog, _, _ := k.GetStringValue("ProgId")
		k.Close()
		if strings.EqualFold(prog, progID) || strings.EqualFold(prog, "Applications\\"+base) {
			_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\.module\UserChoice`)
		}
	}

	notifyShellAssocChanged()
	return nil
}
