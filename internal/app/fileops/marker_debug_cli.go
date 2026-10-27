package fileops

import (
	appunit "dow-mod-studio/internal/app/unit"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/unitdef"
	"fmt"
	"github.com/fogleman/fauxgl"
	"os"
	"path/filepath"
	"strings"
)

type MarkerDebugReport struct {
	Module    string                `json:"module"`
	Unit      string                `json:"unit"`
	Model     string                `json:"model"`
	Center    [3]float64            `json:"center"`
	Bounds    MarkerDebugBounds     `json:"bounds"`
	Materials []MarkerDebugMaterial `json:"materials"`
	Markers   []MarkerDebugItem     `json:"markers"`
}

type MarkerDebugMaterial struct {
	Name                string `json:"name"`
	TexturePath         string `json:"texture_path"`
	Found               bool   `json:"found"`
	FoundAtMaterialPath bool   `json:"found_at_material_path"`
	Applied             bool   `json:"applied"`
	ResolvedPath        string `json:"resolved_path,omitempty"`
	Error               string `json:"error,omitempty"`
}

type MarkerDebugBounds struct {
	Min [3]float32 `json:"min"`
	Max [3]float32 `json:"max"`
}

type MarkerDebugItem struct {
	Index      int        `json:"index"`
	Name       string     `json:"name"`
	Parent     string     `json:"parent"`
	Position   [3]float64 `json:"position"`
	Forward    [3]float64 `json:"forward"`
	Next       [3]float64 `json:"next"`
	HasForward bool       `json:"has_forward"`
}

func MarkerDebugResolve(tree *modtree.ModTree, query string) (modelPath, unitPath, unitName string, err error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return "", "", "", fmt.Errorf("empty --unit")
	}
	if file := tree.FindFile(query); file != nil && strings.EqualFold(filepath.Ext(file.Name), ".whm") {
		return file.FullPath, "", strings.TrimSuffix(file.Name, filepath.Ext(file.Name)), nil
	}
	if file := tree.FindFile(query); file != nil && strings.EqualFold(filepath.Ext(file.Name), ".rgd") {
		unitPath = file.FullPath
		unitName = strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
	} else {
		name := strings.TrimSuffix(filepath.Base(strings.ReplaceAll(query, "\\", "/")), filepath.Ext(query))
		if name == "." || name == "" {
			name = query
		}
		matches := tree.Search(name, modtree.SearchRgd, 0)
		for _, hit := range matches {
			if strings.EqualFold(strings.TrimSuffix(hit.File.Name, filepath.Ext(hit.File.Name)), name) {
				if unitPath != "" {
					return "", "", "", fmt.Errorf("unit name %q is ambiguous; pass its .rgd path", query)
				}
				unitPath = hit.File.FullPath
				unitName = strings.TrimSuffix(hit.File.Name, filepath.Ext(hit.File.Name))
			}
		}
		if unitPath == "" {
			return "", "", "", fmt.Errorf("unit %q not found as a .rgd in module", query)
		}
	}

	entry := unitdef.UnitEntry{
		Path: unitPath,
		Name: unitName,
	}
	parts := strings.Split(strings.ReplaceAll(unitPath, "/", "\\"), "\\")
	for i := range parts {
		if strings.EqualFold(parts[i], "races") && i+1 < len(parts) {
			entry.Race = parts[i+1]
			if i+2 < len(parts) {
				switch strings.ToLower(parts[i+2]) {
				case "troops":
					entry.Kind = unitdef.UnitKindTroop
				case "structures":
					entry.Kind = unitdef.UnitKindStructure
				}
			}
			break
		}
	}
	data, readErr := tree.ReadFile(unitPath)
	if readErr != nil {
		return "", "", "", fmt.Errorf("read unit %q: %w", unitPath, readErr)
	}
	rgdFile, parseErr := rgd.ParseRgd(data)
	if parseErr != nil {
		return "", "", "", fmt.Errorf("parse unit %q: %w", unitPath, parseErr)
	}
	source := appunit.UnitModelSource{Unit: unitPath, Name: unitName, Race: entry.Race, Kind: entry.Kind.Folder, Animator: appunit.UnitAnimatorName(rgdFile)}
	modelPath = appunit.UnitModelPath(tree, source)
	if modelPath == "" {
		return "", "", "", fmt.Errorf("no .whm model found for unit %q (%s)", unitName, unitPath)
	}
	return modelPath, unitPath, unitName, nil
}

func ReadModelMaterialAtAuthoredPath(tree *modtree.ModTree, roots []string, authoredPath string) ([]byte, string, bool) {
	if tree != nil {
		if file := tree.FindFile(authoredPath); file != nil {
			if data, err := tree.ReadTreeFile(file); err == nil {
				return data, file.FullPath, true
			}
		}
	}
	rel := filepath.FromSlash(strings.ReplaceAll(authoredPath, "\\", "/"))
	for _, root := range roots {
		path := filepath.Join(root, rel)
		if data, err := os.ReadFile(path); err == nil {
			return data, path, true
		}
	}
	return nil, "", false
}

func VectorArray(v fauxgl.Vector) [3]float64 { return [3]float64{v.X, v.Y, v.Z} }
