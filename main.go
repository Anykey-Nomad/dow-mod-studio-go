package main

import (
	"dow-mod-studio/internal/app"
	"dow-mod-studio/internal/applog"
)

// The program itself lives in internal/app: this file is the whole root
// package, so the executable still builds from `go build .` while the root
// holds one file instead of a hundred.
func main() {
	// Last resort: anything that escapes app.Run's own guards is logged with
	// a full stack trace instead of vanishing into a stderr no console reads.
	defer applog.HandlePanic()
	app.Run()
}
