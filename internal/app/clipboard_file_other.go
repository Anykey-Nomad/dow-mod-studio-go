//go:build !windows

package app

import (
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"gioui.org/io/clipboard"
	"gioui.org/layout"
)

// copyFileToClipboard offers the file to the system clipboard as a
// text/uri-list — the MIME type file managers read for a file drop — so
// pasting into a file manager puts the log file there. Windows takes the
// real file handle instead; see clipboard_file_windows.go.
func copyFileToClipboard(gtx layout.Context, path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	uri := (&url.URL{Scheme: "file", Path: abs}).String()
	gtx.Execute(clipboard.WriteCmd{
		Type: "text/uri-list",
		Data: io.NopCloser(strings.NewReader(uri + "\r\n")),
	})
	return nil
}
