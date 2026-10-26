package main

import "dow-mod-studio/internal/app"

// The program itself lives in internal/app: this file is the whole root
// package, so the executable still builds from `go build .` while the root
// holds one file instead of a hundred.
func main() {
	app.Run()
}
