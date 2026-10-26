package unitdef_test

import (
	"testing"

	"dow-mod-studio/internal/modtree"
)

// testDXP3Module is the stock Definitive Edition module the data tests read.
const testDXP3Module = `C:/Program Files/Steam/steamapps/common/Dawn of War Definitive Edition/DXP3.module`

// openTestDXP3 opens the stock module for a data test, skipping when the
// game is not installed.
func openTestDXP3(t *testing.T) *modtree.ModTree {
	t.Helper()
	tree, err := modtree.BuildModTree(testDXP3Module, "English")
	if err != nil {
		t.Skipf("DXP3 module not available: %v", err)
	}
	t.Cleanup(tree.Close)
	return tree
}
