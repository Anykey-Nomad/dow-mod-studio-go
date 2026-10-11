package main

import "testing"

func TestCleanDialogPathStripsSurroundingQuotes(t *testing.T) {
	cases := map[string]string{
		`"C:\Game\DoWDE.module"`: `C:\Game\DoWDE.module`,
		`  "C:\Game"  `:          `C:\Game`,
		`C:\Game\DoWDE.module`:   `C:\Game\DoWDE.module`,
		`"C:\Game`:               `"C:\Game`,
	}
	for in, want := range cases {
		if got := cleanDialogPath(in); got != want {
			t.Errorf("cleanDialogPath(%q) = %q, want %q", in, got, want)
		}
	}
}
