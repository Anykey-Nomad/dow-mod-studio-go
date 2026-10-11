package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	logFile      *os.File
	logFilePath  string
	logMu        sync.Mutex
	logWriter    io.Writer
	logCloseOnce sync.Once
)

// ── levels — INFO / WARN / ERROR ────────────────────────────────────────────
// Helpers prepend a level prefix; they use the same logger output (file+stderr)
// and Lshortfile so the call site is still visible. No filtering by default —
// all levels are emitted — but a future filter can check currentLogLevel.

type logLevel int

const (
	LevelDebug logLevel = iota
	LevelInfo
	LevelWarn
	LevelError
)

var currentLogLevel logLevel = LevelInfo

func SetLogLevel(l logLevel)    { currentLogLevel = l }
func CurrentLogLevel() logLevel { return currentLogLevel }

func levelEnabled(l logLevel) bool { return l >= currentLogLevel }

func LogDebug(format string, args ...any) {
	if !levelEnabled(LevelDebug) {
		return
	}
	log.Printf("DEBUG: "+format, args...)
}
func LogInfo(format string, args ...any) {
	if !levelEnabled(LevelInfo) {
		return
	}
	log.Printf("INFO: "+format, args...)
}
func LogWarn(format string, args ...any) {
	if !levelEnabled(LevelWarn) {
		return
	}
	log.Printf("WARN: "+format, args...)
}
func LogError(format string, args ...any) {
	if !levelEnabled(LevelError) {
		return
	}
	log.Printf("ERROR: "+format, args...)
}

// legacy aliases (lowercase) for internal use
func logInfof(format string, args ...any)  { LogInfo(format, args...) }
func logWarnf(format string, args ...any)  { LogWarn(format, args...) }
func logErrorf(format string, args ...any) { LogError(format, args...) }

// logDir returns a writable log directory, trying exe dir, cwd, temp in order.
func logDir() string {
	candidates := []string{}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), "log"))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, filepath.Join(cwd, "log"))
	}
	candidates = append(candidates, filepath.Join(os.TempDir(), "dow-mod-studio-go-log"))
	for _, dir := range candidates {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			continue
		}
		// probe writable
		probe := filepath.Join(dir, ".writetest")
		if f, err := os.OpenFile(probe, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644); err == nil {
			f.Close()
			_ = os.Remove(probe)
			return dir
		}
	}
	// fallback to first
	if len(candidates) > 0 {
		_ = os.MkdirAll(candidates[0], 0o755)
		return candidates[0]
	}
	return "log"
}

func initLogger() {
	dir := logDir()
	_ = os.MkdirAll(dir, 0o755)
	ts := time.Now().Format("2006-01-02_15-04-05")
	pid := os.Getpid()
	name := fmt.Sprintf("%s_%d.log", ts, pid)
	path := filepath.Join(dir, name)
	if _, err := os.Stat(path); err == nil {
		// very unlikely: same second + same pid (pid reuse); add millis
		ts2 := time.Now().Format("2006-01-02_15-04-05.000")
		name = fmt.Sprintf("%s_%d.log", ts2, pid)
		path = filepath.Join(dir, name)
		for i := 2; i < 10; i++ {
			if _, err := os.Stat(path); err != nil {
				break
			}
			name = fmt.Sprintf("%s_%d_%d.log", ts2, pid, i)
			path = filepath.Join(dir, name)
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "logger: cannot create %q: %v\n", path, err)
		log.SetOutput(os.Stderr)
		log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
		return
	}
	logFile = f
	logFilePath = path
	logWriter = io.MultiWriter(f, os.Stderr)
	log.SetOutput(logWriter)
	log.SetFlags(log.LstdFlags | log.Lmicroseconds | log.Lshortfile)
	cwd, _ := os.Getwd()
	exe, _ := os.Executable()
	log.Printf("=== DoW Mod Studio started pid=%d exe=%q cwd=%q log=%q ===", pid, exe, cwd, path)
	log.Printf("args: %q", os.Args)
	go cleanupOldLogs(dir)
}

func flushLogger() {
	logMu.Lock()
	defer logMu.Unlock()
	if logFile != nil {
		_ = logFile.Sync()
	}
}

func closeLogger() {
	logCloseOnce.Do(func() {
		flushLogger()
		if logFile != nil {
			log.Printf("=== exiting ===")
			_ = logFile.Sync()
			_ = logFile.Close()
			logFile = nil
		}
	})
}

func handlePanic() {
	if r := recover(); r != nil {
		if logFile == nil {
			// try to init so we have a file
			initLogger()
		}
		log.Printf("PANIC: %v", r)
		log.Printf("stack:\n%s", debug.Stack())
		fmt.Fprintf(os.Stderr, "PANIC: %v\n%s\n", r, debug.Stack())
		flushLogger()
		closeLogger()
		os.Exit(2)
	}
}

func exitLogged(code int) {
	flushLogger()
	closeLogger()
	os.Exit(code)
}

// openLogFolder opens the log directory in the system file manager.
// It is safe to call from the UI thread (it spawns explorer/
// xdg-open without blocking).
func openLogFolder() error {
	dir := logDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		LogError("openLogFolder mkdir %q: %v", dir, err)
		return err
	}
	LogInfo("opening log folder %q", dir)
	// Prefer explorer on Windows; fall back to xdg-open/open elsewhere.
	// We deliberately do not import os/exec at top-level for logger alone —
	// caller in main.go uses exec, but logger fallback uses raw StartProcess
	// so logger can stay without exec dependency (avoids import cycle).
	// Try exec via shell if available, otherwise just return dir.
	if exe, err := os.Executable(); err == nil {
		_ = exe
	}
	// Use platform-specific opener launched without blocking.
	// On Windows: explorer.exe <dir>
	// On macOS: open <dir>
	// On Linux: xdg-open <dir>
	var cmd string
	var args []string
	switch {
	case isWindows():
		cmd = "explorer.exe"
		args = []string{dir}
	default:
		// try xdg-open, fall back to open
		if _, err := os.Stat("/usr/bin/xdg-open"); err == nil {
			cmd = "xdg-open"
			args = []string{dir}
		} else {
			cmd = "open"
			args = []string{dir}
		}
	}
	// Use os.StartProcess to avoid importing os/exec in logger.go.
	// We need the full path to the opener - search PATH via LookPath equivalent.
	path, err := lookupPath(cmd)
	if err != nil {
		path = cmd
	}
	proc, err := os.StartProcess(path, append([]string{path}, args...), &os.ProcAttr{Files: []*os.File{nil, nil, nil}})
	if err != nil {
		LogError("openLogFolder StartProcess %q: %v", path, err)
		return err
	}
	_ = proc.Release()
	return nil
}

func openFileWithDefaultApp(path string) error {
	var cmd string
	var args []string
	if isWindows() {
		cmd = "explorer.exe"
		args = []string{path}
	} else if _, err := os.Stat("/usr/bin/xdg-open"); err == nil {
		cmd = "xdg-open"
		args = []string{path}
	} else {
		cmd = "open"
		args = []string{path}
	}
	resolved, err := lookupPath(cmd)
	if err != nil {
		resolved = cmd
	}
	proc, err := os.StartProcess(resolved, append([]string{resolved}, args...), &os.ProcAttr{Files: []*os.File{nil, nil, nil}})
	if err != nil {
		return err
	}
	_ = proc.Release()
	return nil
}

func openRecycleFolder() error {
	dir, err := ensureRecycleBinDir()
	if err != nil {
		return err
	}
	LogInfo("opening recycle bin folder %q", dir)
	var cmd string
	var args []string
	if isWindows() {
		cmd = "explorer.exe"
		args = []string{dir}
	} else if _, err := os.Stat("/usr/bin/xdg-open"); err == nil {
		cmd = "xdg-open"
		args = []string{dir}
	} else {
		cmd = "open"
		args = []string{dir}
	}
	path, err := lookupPath(cmd)
	if err != nil {
		path = cmd
	}
	proc, err := os.StartProcess(path, append([]string{path}, args...), &os.ProcAttr{Files: []*os.File{nil, nil, nil}})
	if err != nil {
		return err
	}
	_ = proc.Release()
	return nil
}

func isWindows() bool {
	return fmt.Sprintf("%c", filepath.Separator) == "\\"
}

func lookupPath(file string) (string, error) {
	// minimal LookPath without importing os/exec
	if filepath.IsAbs(file) {
		return file, nil
	}
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		pathEnv = os.Getenv("Path")
	}
	for _, dir := range filepath.SplitList(pathEnv) {
		if dir == "" {
			dir = "."
		}
		full := filepath.Join(dir, file)
		if _, err := os.Stat(full); err == nil {
			return full, nil
		}
		// on Windows, also try .exe suffix
		if isWindows() && !hasSuffixFold(full, ".exe") {
			if _, err := os.Stat(full + ".exe"); err == nil {
				return full + ".exe", nil
			}
		}
	}
	return "", fmt.Errorf("executable %q not found in PATH", file)
}

func hasSuffixFold(s, suf string) bool {
	if len(s) < len(suf) {
		return false
	}
	return strings.EqualFold(s[len(s)-len(suf):], suf)
}

func cleanupOldLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	type ent struct {
		path string
		mod  time.Time
		size int64
	}
	var list []ent
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".log" {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		list = append(list, ent{path: filepath.Join(dir, e.Name()), mod: info.ModTime(), size: info.Size()})
	}
	if len(list) <= 100 {
		// also check total size ~50 MB
		var total int64
		for _, v := range list {
			total += v.size
		}
		if total < 50*1024*1024 {
			return
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].mod.Before(list[j].mod) })
	// keep newest 80, delete oldest
	keep := 80
	if len(list) <= keep {
		return
	}
	toDelete := list[:len(list)-keep]
	for _, v := range toDelete {
		_ = os.Remove(v.path)
		log.Printf("cleanupOldLogs: removed old %q", v.path)
	}
	flushLogger()
}
