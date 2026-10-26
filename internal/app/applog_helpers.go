package app

import "dow-mod-studio/internal/applog"

// The application log — its levels, its file under the user's config
// directory, its panic handling and its folder-opening helpers — lives in
// internal/applog together with the collector tuning. The rest of the program
// logs through the names it always had, so call sites and the tests read
// unchanged and a later move of the logger touches only this file.

// ── types and levels ─────────────────────────────────────────────────────────

type LogLevel = applog.LogLevel

const (
	LevelDebug = applog.LevelDebug
	LevelInfo  = applog.LevelInfo
	LevelWarn  = applog.LevelWarn
	LevelError = applog.LevelError
)

func SetLogLevel(l LogLevel)    { applog.SetLogLevel(l) }
func CurrentLogLevel() LogLevel { return applog.CurrentLogLevel() }

// ── logging ──────────────────────────────────────────────────────────────────

func LogDebug(format string, args ...any) { applog.LogDebug(format, args...) }
func LogInfo(format string, args ...any)  { applog.LogInfo(format, args...) }
func LogWarn(format string, args ...any)  { applog.LogWarn(format, args...) }
func LogError(format string, args ...any) { applog.LogError(format, args...) }

// ── lifecycle ────────────────────────────────────────────────────────────────

func initLogger()         { applog.InitLogger() }
func flushLogger()        { applog.FlushLogger() }
func closeLogger()        { applog.CloseLogger() }
func handlePanic()        { applog.HandlePanic() }
func exitLogged(code int) { applog.ExitLogged(code) }
func tuneCollector()      { applog.TuneCollector() }

// ── folders and paths ────────────────────────────────────────────────────────

func logDir() string { return applog.LogDir() }

func openLogFolder() error                  { return applog.OpenLogFolder() }
func openRecycleFolder() error              { return applog.OpenRecycleFolder() }
func openFileWithDefaultApp(p string) error { return applog.OpenFileWithDefaultApp(p) }

func recycleBinDir() (string, error)       { return applog.RecycleBinDir() }
func ensureRecycleBinDir() (string, error) { return applog.EnsureRecycleBinDir() }
