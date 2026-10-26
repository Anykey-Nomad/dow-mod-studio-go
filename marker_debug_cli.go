package main

import (
	engine "dow-mod-studio/internal/3d_engine"
	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/raster"
	"dow-mod-studio/internal/rgd"
	"dow-mod-studio/internal/whm"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/fogleman/fauxgl"
)

type markerDebugReport struct {
	Module    string                `json:"module"`
	Unit      string                `json:"unit"`
	Model     string                `json:"model"`
	Center    [3]float64            `json:"center"`
	Bounds    markerDebugBounds     `json:"bounds"`
	Materials []markerDebugMaterial `json:"materials"`
	Markers   []markerDebugItem     `json:"markers"`
}

type markerDebugMaterial struct {
	Name                string `json:"name"`
	TexturePath         string `json:"texture_path"`
	Found               bool   `json:"found"`
	FoundAtMaterialPath bool   `json:"found_at_material_path"`
	Applied             bool   `json:"applied"`
	ResolvedPath        string `json:"resolved_path,omitempty"`
	Error               string `json:"error,omitempty"`
}

type markerDebugBounds struct {
	Min [3]float32 `json:"min"`
	Max [3]float32 `json:"max"`
}

type markerDebugItem struct {
	Index      int        `json:"index"`
	Name       string     `json:"name"`
	Parent     string     `json:"parent"`
	Position   [3]float64 `json:"position"`
	Forward    [3]float64 `json:"forward"`
	Next       [3]float64 `json:"next"`
	HasForward bool       `json:"has_forward"`
}

func runMarkerDebugCLI(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("--debug-markers", flag.ContinueOnError)
	flags.SetOutput(stderr)
	module := flags.String("module", "", "path to a .module file")
	unit := flags.String("unit", "", "unit name, unit .rgd virtual path, or model .whm virtual path")
	locale := flags.String("locale", "English", "module locale")
	step := flags.Float64("step", 1, "forward endpoint distance in model units")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *module == "" || *unit == "" {
		fmt.Fprintln(stderr, "usage: dow-mod-studio.exe --debug-markers --module <file.module> --unit <name|path> [--locale English] [--step 1]")
		return 2
	}
	if math.IsNaN(*step) || math.IsInf(*step, 0) || *step <= 0 {
		fmt.Fprintln(stderr, "--step must be a positive finite number")
		return 2
	}

	tree, err := modtree.BuildModTree(*module, *locale)
	if err != nil {
		fmt.Fprintf(stderr, "load module: %v\n", err)
		return 1
	}
	defer tree.Close()

	modelPath, unitPath, unitName, err := markerDebugResolve(tree, *unit)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	raw, err := tree.ReadFile(modelPath)
	if err != nil {
		fmt.Fprintf(stderr, "read model %q: %v\n", modelPath, err)
		return 1
	}
	model, err := whm.Parse(raw)
	if err != nil {
		fmt.Fprintf(stderr, "parse model %q: %v\n", modelPath, err)
		return 1
	}

	pose := model.RestPose()
	report := markerDebugReport{
		Module:    tree.ModFilePath,
		Unit:      unitName,
		Model:     modelPath,
		Bounds:    markerDebugBounds{Min: model.BoundMin, Max: model.BoundMax},
		Materials: make([]markerDebugMaterial, 0),
		Markers:   make([]markerDebugItem, 0, len(model.Markers)),
	}
	center, _ := engine.ModelCenter(model)
	report.Center = vectorArray(center)
	report.Materials = markerDebugMaterials(tree, modelPath, model)
	for i := range model.Markers {
		marker := &model.Markers[i]
		pos, posOK := model.MarkerWorld(i, pose, true)
		if !posOK {
			continue
		}
		item := markerDebugItem{
			Index:    i,
			Name:     marker.Name,
			Parent:   marker.Parent,
			Position: vectorArray(pos),
		}
		if axis, ok := model.MarkerAxis(i, pose, true); ok {
			item.HasForward = true
			item.Forward = vectorArray(axis)
			item.Next = vectorArray(pos.Add(axis.MulScalar(*step)))
		}
		report.Markers = append(report.Markers, item)
	}
	if err := json.NewEncoder(stdout).Encode(report); err != nil {
		fmt.Fprintf(stderr, "write marker report: %v\n", err)
		return 1
	}
	_ = unitPath // retained by resolve for diagnostics and future path output
	return 0
}

func markerDebugResolve(tree *modtree.ModTree, query string) (modelPath, unitPath, unitName string, err error) {
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

	entry := unitEntry{
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
					entry.Kind = unitKindTroop
				case "structures":
					entry.Kind = unitKindStructure
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
	source := unitModelSource{unit: unitPath, name: unitName, race: entry.Race, kind: entry.Kind.Folder, animator: unitAnimatorName(rgdFile)}
	modelPath = unitModelPath(tree, source)
	if modelPath == "" {
		return "", "", "", fmt.Errorf("no .whm model found for unit %q (%s)", unitName, unitPath)
	}
	return modelPath, unitPath, unitName, nil
}

func markerDebugMaterials(tree *modtree.ModTree, modelPath string, model *whm.Model) []markerDebugMaterial {
	if model == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []markerDebugMaterial
	for _, mesh := range model.Meshes {
		name := strings.TrimSpace(mesh.Material)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		status := markerDebugMaterial{Name: name, TexturePath: name + ".rsh"}
		roots := materialSearchRoots(tree)
		data, found := readModelMaterial(tree, roots, modelPath, name)
		status.Found = found
		if exact, path, exactOK := readModelMaterialAtAuthoredPath(tree, roots, status.TexturePath); exactOK {
			data, status.FoundAtMaterialPath, status.ResolvedPath = exact, true, path
		}
		if !found {
			status.Error = "texture file was not found"
			out = append(out, status)
			continue
		}
		if status.ResolvedPath == "" {
			status.ResolvedPath = "resolved by fallback search"
		}
		if _, _, err := raster.RshRawImage(data, 0); err != nil {
			status.Error = "texture decode failed: " + err.Error()
			out = append(out, status)
			continue
		}
		if _, _, err := decodeRshImage(data, 0); err != nil {
			status.Error = "texture decode failed: " + err.Error()
			out = append(out, status)
			continue
		}
		status.Applied = true
		out = append(out, status)
	}
	return out
}

func readModelMaterialAtAuthoredPath(tree *modtree.ModTree, roots []string, authoredPath string) ([]byte, string, bool) {
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

func vectorArray(v fauxgl.Vector) [3]float64 { return [3]float64{v.X, v.Y, v.Z} }
