package modtree

import "strings"

// VirtualDirAndBase splits a backslash virtual path into dir and base name.
func VirtualDirAndBase(virtualPath string) (string, string) {
	normalized := strings.ReplaceAll(virtualPath, "/", `\`)
	pos := strings.LastIndex(normalized, `\`)
	if pos < 0 {
		return "", normalized
	}
	return normalized[:pos], normalized[pos+1:]
}

// FindFolderByPath resolves a backslash separated path under root.
func FindFolderByPath(root *TreeFolder, path string) *TreeFolder {
	cur := root
	for _, seg := range strings.Split(path, "\\") {
		if seg == "" {
			continue
		}
		cur = FindFolderByLower(cur, seg)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// FindFolderByLower looks up a direct child folder by case-insensitive name.
func FindFolderByLower(folder *TreeFolder, name string) *TreeFolder {
	if folder == nil {
		return nil
	}
	if f, ok := folder.folderByLower[strings.ToLower(name)]; ok {
		return f
	}
	return nil
}
