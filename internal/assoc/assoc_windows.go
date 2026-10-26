//go:build windows

package assoc

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Per-user file associations (HKCU\Software\Classes), so neither registration
// nor its removal needs elevation. Every format works the same way —
// extension → ProgID → open command — so one implementation serves the
// .module the File menu registers and the formats the Settings dialog offers.

// getExePath returns the absolute path of current executable.
func GetExePath() string {
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

// associationsSupported reports whether the platform has per-user file
// associations to manage. Windows does; the stub build does not, and its
// settings dialog leaves the section out.
func AssociationsSupported() bool { return true }

func NotifyShellAssocChanged() {
	// SHCNE_ASSOCCHANGED = 0x08000000, SHCNF_IDLIST = 0x0000
	dll := windows.NewLazySystemDLL("shell32.dll")
	proc := dll.NewProc("SHChangeNotify")
	_, _, _ = proc.Call(uintptr(0x08000000), uintptr(0x0000), 0, 0)
}

// isExtAssociated checks whether double-clicking the format will launch this
// exe. On Win10/11 UserChoice overrides HKCU\Software\Classes\<ext>, so we
// check it first.
func IsExtAssociated(t AssocType) (bool, string, error) {
	exe := GetExePath()
	if exe == "" {
		return false, "", fmt.Errorf("cannot get exe path")
	}
	lowerExe := strings.ToLower(exe)
	base := filepath.Base(exe)
	appProg := "Applications\\" + base
	appCmdKey := `Software\Classes\Applications\` + base + `\shell\open\command`
	extKey := `Software\Classes\` + t.Ext
	progKey := `Software\Classes\` + t.ProgID
	commandKey := progKey + `\shell\open\command`

	// 1) If UserChoice exists, it determines what double-click does.
	if ku, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\UserChoice`, registry.QUERY_VALUE); err == nil {
		prog, _, _ := ku.GetStringValue("ProgId")
		ku.Close()
		if prog != "" {
			if strings.EqualFold(prog, t.ProgID) {
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
			// UserChoice points elsewhere → double-click will NOT use us
			return false, fmt.Sprintf("UserChoice=%q overrides (expected %q)", prog, t.ProgID), nil
		}
		// empty ProgId → treat as not associated, fall through to HKCU check
	}

	// 2) No UserChoice or empty → HKCU\Software\Classes\<ext> decides.
	k, err := registry.OpenKey(registry.CURRENT_USER, extKey, registry.QUERY_VALUE)
	if err != nil {
		return false, "", nil
	}
	prog, _, err := k.GetStringValue("")
	k.Close()
	if err != nil || !strings.EqualFold(prog, t.ProgID) {
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

// registerExtAssociation creates a per-user file association for one format.
// Uses HKCU\Software\Classes which does NOT require elevation on Windows.
func RegisterExtAssociation(t AssocType) error {
	exe := GetExePath()
	if exe == "" {
		return fmt.Errorf("cannot determine executable path")
	}
	command := `\"` + exe + `\" \"%1\"`
	base := filepath.Base(exe)
	extKey := `Software\Classes\` + t.Ext
	progKey := `Software\Classes\` + t.ProgID
	commandKey := progKey + `\shell\open\command`

	// <ext> → ProgID
	key, _, err := registry.CreateKey(registry.CURRENT_USER, extKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", extKey, err)
	}
	if err := key.SetStringValue("", t.ProgID); err != nil {
		key.Close()
		return err
	}
	key.Close()

	// ProgID description
	key2, _, err := registry.CreateKey(registry.CURRENT_USER, progKey, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", progKey, err)
	}
	_ = key2.SetStringValue("", t.Desc)
	key2.Close()

	// DefaultIcon → "<exe>",0
	keyIcon, _, err := registry.CreateKey(registry.CURRENT_USER, progKey+`\DefaultIcon`, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("create %s: %w", progKey+`\DefaultIcon`, err)
	}
	_ = keyIcon.SetStringValue("", `\"`+exe+`\",0`)
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
	keyOpen, _, err := registry.CreateKey(registry.CURRENT_USER, progKey+`\shell\open`, registry.SET_VALUE)
	if err == nil {
		_ = keyOpen.SetStringValue("", "Open with DoW Mod Studio")
		keyOpen.Close()
	}

	// Make Explorer offer us in Open With and resolve double-click even if UserChoice is absent
	if k, _, err := registry.CreateKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\OpenWithProgids`, registry.SET_VALUE); err == nil {
		_ = k.SetStringValue(t.ProgID, "")
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
		_ = k.SetStringValue("", t.Desc)
		k.Close()
	}

	// Remove UserChoice if it does NOT point to us — otherwise HKCU Classes is ignored on Win10/11
	// We cannot compute the Hash, so we delete the key and let Explorer fall back to the ProgID.
	if ku, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\UserChoice`, registry.QUERY_VALUE); err == nil {
		prog, _, _ := ku.GetStringValue("ProgId")
		ku.Close()
		if !strings.EqualFold(prog, t.ProgID) {
			_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\UserChoice`)
		}
	} else {
		// No UserChoice or cannot open — still try to delete (may have been stale)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\UserChoice`)
	}

	// Notify shell so Explorer picks up immediately without waiting seconds
	NotifyShellAssocChanged()

	return nil
}

// unregisterExtAssociation removes a per-user association when it belongs to
// this app. It is careful not to delete an association that points elsewhere.
func UnregisterExtAssociation(t AssocType) error {
	base := filepath.Base(GetExePath())
	extKey := `Software\Classes\` + t.Ext
	progKey := `Software\Classes\` + t.ProgID
	commandKey := progKey + `\shell\open\command`

	// Only delete <ext> if it points to us
	if k, err := registry.OpenKey(registry.CURRENT_USER, extKey, registry.QUERY_VALUE); err == nil {
		prog, _, _ := k.GetStringValue("")
		k.Close()
		if strings.EqualFold(prog, t.ProgID) {
			_ = registry.DeleteKey(registry.CURRENT_USER, extKey)
		}
	}
	// Delete ProgID tree bottom-up (DeleteKey fails if subkeys remain)
	_ = registry.DeleteKey(registry.CURRENT_USER, commandKey)
	_ = registry.DeleteKey(registry.CURRENT_USER, progKey+`\shell\open`)
	_ = registry.DeleteKey(registry.CURRENT_USER, progKey+`\shell`)
	_ = registry.DeleteKey(registry.CURRENT_USER, progKey+`\DefaultIcon`)
	_ = registry.DeleteKey(registry.CURRENT_USER, progKey)
	// The Applications\<exe> fallback entry is shared by every format, so it
	// goes only once no format of ours is left registered.
	if base != "" && !AnyOtherAssocRegistered(t.Ext) {
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base+`\shell\open\command`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base+`\shell\open`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base+`\shell`)
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Classes\Applications\`+base)
	}

	// Remove from OpenWithProgids
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\OpenWithProgids`, registry.SET_VALUE); err == nil {
		_ = k.DeleteValue(t.ProgID)
		k.Close()
	}
	// Clean UserChoice if it points to us (or to our Applications entry)
	if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\UserChoice`, registry.QUERY_VALUE); err == nil {
		prog, _, _ := k.GetStringValue("ProgId")
		k.Close()
		if strings.EqualFold(prog, t.ProgID) || strings.EqualFold(prog, "Applications\\"+base) {
			_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Explorer\FileExts\`+t.Ext+`\UserChoice`)
		}
	}

	NotifyShellAssocChanged()
	return nil
}

// anyOtherAssocRegistered reports whether a format other than excludeExt is
// still registered to this exe in HKCU\Software\Classes.
func AnyOtherAssocRegistered(excludeExt string) bool {
	for _, t := range append([]AssocType{ModuleAssoc}, AssocTypes...) {
		if strings.EqualFold(t.Ext, excludeExt) {
			continue
		}
		if k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Classes\`+t.Ext, registry.QUERY_VALUE); err == nil {
			prog, _, _ := k.GetStringValue("")
			k.Close()
			if strings.EqualFold(prog, t.ProgID) {
				return true
			}
		}
	}
	return false
}
