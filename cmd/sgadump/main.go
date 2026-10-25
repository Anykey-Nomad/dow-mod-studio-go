// Command sgadump walks an .sga archive and prints what is inside it.
//
// The behaviour scripts of the game (the Lua files that decide when a
// building shows its addons) live in archives, not in folders, so they are
// not part of the module tree. This tool is how they are read.
//
//	go run ./cmd/sgadump "C:\...\W40kData.sga" -find monolith
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dow-mod-studio/internal/sga"
)

func main() {
	path := flag.String("archive", "", "путь к архиву .sga")
	find := flag.String("find", "", "печатать только файлы, в имени которых есть подстрока")
	dir := flag.String("dir", "", "напечатать содержимое папки архива")
	cat := flag.String("cat", "", "напечатать файл архива")
	flag.Parse()
	if *path == "" {
		fmt.Fprintln(os.Stderr, "нужен -archive")
		os.Exit(2)
	}
	f, err := sga.OpenSGA(*path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open:", err)
		os.Exit(1)
	}
	defer f.Close()

	switch {
	case *cat != "":
		catFile(f, *cat)
	case *dir != "":
		listDir(f, *dir)
	default:
		for ti := 0; ti < f.ToCCount(); ti++ {
			fmt.Printf("=== %s ===\n", f.ToCName(ti))
			root, err := f.RootDir(ti)
			if err != nil {
				continue
			}
			walk(f, root, "", *find, 0)
		}
	}
}

// walk обходит дерево архива и печатает файлы, подходящие под фильтр.
func walk(f *sga.SGAFile, dirID int, prefix, find string, depth int) {
	if depth > 8 {
		return
	}
	entries, err := f.List(dirID)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := filepath.Join(prefix, e.Name)
		if e.IsDir {
			walk(f, e.DirID, name, find, depth+1)
			continue
		}
		if find == "" || strings.Contains(strings.ToLower(name), strings.ToLower(find)) {
			fmt.Println(name)
		}
	}
}

// listDir печатает содержимое папки архива.
func listDir(f *sga.SGAFile, path string) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) == 0 {
		return
	}
	for ti := 0; ti < f.ToCCount(); ti++ {
		dirID, err := f.RootDir(ti)
		if err != nil {
			continue
		}
		for _, part := range parts {
			entries, err := f.List(dirID)
			if err != nil {
				return
			}
			found := false
			for _, e := range entries {
				if e.IsDir && strings.EqualFold(e.Name, part) {
					dirID = e.DirID
					found = true
					break
				}
			}
			if !found {
				return
			}
		}
		entries, err := f.List(dirID)
		if err != nil {
			return
		}
		for _, e := range entries {
			kind := "file"
			if e.IsDir {
				kind = "dir "
			}
			fmt.Printf("%s %s\n", kind, e.Name)
		}
	}
}

// catFile печатает содержимое файла архива.
func catFile(f *sga.SGAFile, path string) {
	parts := strings.Split(filepath.ToSlash(path), "/")
	name := parts[len(parts)-1]
	dirPath := parts[:len(parts)-1]
	for ti := 0; ti < f.ToCCount(); ti++ {
		dirID, err := f.RootDir(ti)
		if err != nil {
			continue
		}
		ok := true
		for _, part := range dirPath {
			entries, err := f.List(dirID)
			if err != nil {
				ok = false
				break
			}
			found := false
			for _, e := range entries {
				if e.IsDir && strings.EqualFold(e.Name, part) {
					dirID = e.DirID
					found = true
					break
				}
			}
			if !found {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		entries, err := f.List(dirID)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir || !strings.EqualFold(e.Name, name) {
				continue
			}
			data, err := f.ReadFile(e.FileID)
			if err != nil {
				fmt.Fprintln(os.Stderr, "read:", err)
				continue
			}
			fmt.Printf("=== %s (%d байт) ===\n", path, len(data))
			fmt.Println(string(data))
			return
		}
	}
	fmt.Fprintln(os.Stderr, "файл не найден:", path)
}
