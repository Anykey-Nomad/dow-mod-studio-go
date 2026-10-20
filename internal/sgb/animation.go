package sgb

import (
	"fmt"
	"math"
	"strings"

	"dow-mod-studio/internal/modtree"
	"dow-mod-studio/internal/whe"
	"dow-mod-studio/internal/whm"
	"github.com/fogleman/fauxgl"
)

// AnimatedEntity is one placed object. Its source model can be shared by all
// copies of the same blueprint; Animation and Time belong to the placement.
type AnimatedEntity struct {
	ID        int
	BoneBase  int
	Placement Placement
	Model     *whm.Model
	Animation *whm.Animation
	Time      float32
}

// EntityPose evaluates this instance's clip at its own normalized playhead.
func EntityPose(entity AnimatedEntity) whm.Pose {
	if entity.Model == nil {
		return whm.Pose{}
	}
	return entity.Model.PoseAt(entity.Animation, entity.Time)
}

// BindAnimationToModel returns a shallow animation copy whose bone tracks are
// indexed against model's skeleton. A linked clip is parsed against the linked
// source skeleton, but the placed geometry is skinned by its own model; track
// names, not the source's numeric bone indices, identify the corresponding
// target bones.
func BindAnimationToModel(model *whm.Model, animation *whm.Animation) *whm.Animation {
	if model == nil || animation == nil {
		return nil
	}
	bound := *animation
	bound.Bones = append([]whm.Track(nil), animation.Bones...)
	boneIndices := make(map[string]int, len(model.Bones))
	for i, bone := range model.Bones {
		name := strings.ToLower(strings.TrimSpace(bone.Name))
		if name != "" {
			if _, exists := boneIndices[name]; !exists {
				boneIndices[name] = i
			}
		}
	}
	for i := range bound.Bones {
		name := strings.ToLower(strings.TrimSpace(bound.Bones[i].Name))
		if name == "" {
			if bound.Bones[i].Bone < 0 || bound.Bones[i].Bone >= len(model.Bones) {
				bound.Bones[i].Bone = -1
			}
			continue
		}
		if bone, exists := boneIndices[name]; exists {
			bound.Bones[i].Bone = bone
		} else {
			bound.Bones[i].Bone = -1
		}
	}
	return &bound
}

// CombineEntityPoses combines each independently evaluated pose with its
// instance placement. Scene vertices are baked into world coordinates, so each
// skin matrix is conjugated by placement to leave its bind pose unchanged.
func CombineEntityPoses(boneCount int, entities []AnimatedEntity) whm.Pose {
	pose := whm.Pose{Bones: make([]fauxgl.Matrix, boneCount), BindInv: make([]fauxgl.Matrix, boneCount)}
	for i := 0; i < boneCount; i++ {
		pose.Bones[i], pose.BindInv[i] = fauxgl.Identity(), fauxgl.Identity()
	}
	for _, entity := range entities {
		if entity.Model == nil || entity.BoneBase < 0 {
			continue
		}
		local := EntityPose(entity)
		frame := EntityModelMatrix(entity.Placement)
		inverseFrame := frame.Inverse()
		for bone := range local.Bones {
			dst := entity.BoneBase + bone
			if dst < 0 || dst >= len(pose.Bones) {
				break
			}
			pose.Bones[dst] = frame.Mul(local.Bones[bone])
			pose.BindInv[dst] = local.BindInv[bone].Mul(inverseFrame)
		}
	}
	return pose
}

// AdvanceEntityAnimation wraps one instance's playhead based on authored
// duration. Each entity is stepped separately and can have a different phase.
func AdvanceEntityAnimation(entity *AnimatedEntity, elapsedSeconds float64) {
	if entity == nil || entity.Animation == nil || elapsedSeconds <= 0 || math.IsNaN(elapsedSeconds) || math.IsInf(elapsedSeconds, 0) {
		return
	}
	duration := float64(entity.Animation.Duration)
	if duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return
	}
	entity.Time = float32(math.Mod(float64(entity.Time)+elapsedSeconds/duration, 1))
	if entity.Time < 0 {
		entity.Time++
	}
}

// EntityModelMatrix transforms model-local points to their placed map frame.
func EntityModelMatrix(p Placement) fauxgl.Matrix {
	sx, sy, sz := p.ScaleX, p.ScaleY, p.ScaleZ
	if sx == 0 {
		sx = 1
	}
	if sy == 0 {
		sy = 1
	}
	if sz == 0 {
		sz = 1
	}
	return fauxgl.Translate(fauxgl.Vector{X: p.X, Y: p.Y, Z: p.Z}).Mul(
		fauxgl.Rotate(fauxgl.Vector{Z: 1}, p.Heading).Mul(
			fauxgl.Scale(fauxgl.Vector{X: sx, Y: sy, Z: sz}),
		),
	)
}

// ResolveIdleAnimation follows the reference editor's default action chain
// where available, then falls back to common idle names and pose-changing clips.
func ResolveIdleAnimation(model *whm.Model, wheData []byte) *whm.Animation {
	if model == nil || len(model.Animations) == 0 {
		return nil
	}
	if len(wheData) > 0 {
		if doc, err := whe.ParseDocument(wheData); err == nil {
			if name := doc.IdleAnimation(); name != "" {
				for i := range model.Animations {
					if strings.EqualFold(model.Animations[i].Name, name) {
						return &model.Animations[i]
					}
				}
			}
		}
	}
	for _, preferred := range []string{"idle_1", "idle_2", "idle", "idle_firing", "stand", "standing"} {
		for i := range model.Animations {
			if strings.EqualFold(model.Animations[i].Name, preferred) {
				return &model.Animations[i]
			}
		}
	}
	for i := range model.Animations {
		a := &model.Animations[i]
		if !strings.HasPrefix(strings.ToLower(a.Name), "vis_") && (len(a.Bones) > 0 || len(a.Meshes) > 0 || len(a.Textures) > 0) {
			return a
		}
	}
	return nil
}

// ResolveLinkedIdleAnimation follows imported animation references, returning
// the source model too because the chosen animation pointer refers to its slice.
func ResolveLinkedIdleAnimation(tree *modtree.ModTree, modelPath string, model *whm.Model) (*whm.Animation, *whm.Model) {
	if tree == nil || model == nil {
		return nil, model
	}
	seen := make(map[string]bool)
	var animation *whm.Animation
	for depth := 0; depth < 8; depth++ {
		key := strings.ToLower(modelPath)
		if seen[key] {
			break
		}
		seen[key] = true
		data, _ := readWHEForModel(tree, modelPath)
		animation = ResolveIdleAnimation(model, data)
		if animation == nil {
			return nil, model
		}
		doc, err := whe.ParseDocument(data)
		if err != nil {
			return animation, model
		}
		link, ok := doc.LinkedAnimations()[strings.ToLower(strings.TrimSpace(animation.Name))]
		if !ok {
			return animation, model
		}
		linkedPath, linkedBytes, err := resolveLinkedModelPath(tree, link.SourceModel)
		if err != nil {
			return nil, model
		}
		linkedKey := strings.ToLower(strings.ReplaceAll(linkedPath, "\\", "/"))
		if seen[linkedKey] {
			return nil, model
		}
		linkedModel, err := whm.Parse(linkedBytes)
		if err != nil {
			break
		}
		var linkedAnimation *whm.Animation
		for i := range linkedModel.Animations {
			if strings.EqualFold(linkedModel.Animations[i].Name, link.Animation) {
				linkedAnimation = &linkedModel.Animations[i]
				break
			}
		}
		if linkedAnimation == nil {
			// Keep the last usable clip instead of accidentally switching to
			// an unrelated idle from the linked model.
			return animation, model
		}
		modelPath, model = linkedPath, linkedModel
		animation = linkedAnimation
	}
	return animation, model
}

// ResolveLinkedAnimation follows one named clip and any XREF links it declares.
// It returns the model that owns the final clip, just like ResolveLinkedIdleAnimation.
func ResolveLinkedAnimation(tree *modtree.ModTree, modelPath string, model *whm.Model, animationName string) (*whm.Animation, *whm.Model) {
	if model == nil || strings.TrimSpace(animationName) == "" {
		return nil, model
	}
	seen := make(map[string]bool)
	for depth := 0; depth < 8; depth++ {
		key := strings.ToLower(strings.ReplaceAll(modelPath, "\\", "/"))
		if seen[key] {
			return nil, model
		}
		seen[key] = true
		var animation *whm.Animation
		for i := range model.Animations {
			if strings.EqualFold(model.Animations[i].Name, animationName) {
				animation = &model.Animations[i]
				break
			}
		}
		if animation == nil {
			return nil, model
		}
		if tree == nil {
			return animation, model
		}
		data, _ := readWHEForModel(tree, modelPath)
		doc, err := whe.ParseDocument(data)
		if err != nil {
			return animation, model
		}
		link, ok := doc.LinkedAnimations()[strings.ToLower(strings.TrimSpace(animation.Name))]
		if !ok {
			return animation, model
		}
		linkedPath, linkedBytes, err := resolveLinkedModelPath(tree, link.SourceModel)
		if err != nil {
			return nil, model
		}
		linkedModel, err := whm.Parse(linkedBytes)
		if err != nil {
			return nil, model
		}
		modelPath, model, animationName = linkedPath, linkedModel, link.Animation
	}
	return nil, model
}

// resolveLinkedModelPath uses both exact tree paths and the reference editor's
// "find model below art/ebps by name" convention. XREF sources are not always
// rooted at Data/art/ebps and may contain an extension already.
func resolveLinkedModelPath(tree *modtree.ModTree, source string) (string, []byte, error) {
	candidate := whe.RGDLinkedAnimationPath(source)
	if data, err := tree.ReadFile(candidate); err == nil {
		return candidate, data, nil
	}
	base := pathBase(strings.TrimSuffix(strings.ReplaceAll(strings.TrimSpace(source), "\\", "/"), ext(source))) + ".whm"
	file := tree.FindByName(strings.ToLower(base))
	if file == nil {
		return "", nil, fmt.Errorf("linked model %q not found", source)
	}
	data, err := tree.ReadTreeFile(file)
	if err != nil {
		return "", nil, err
	}
	return file.FullPath, data, nil
}

func readWHEForModel(tree *modtree.ModTree, modelPath string) ([]byte, error) {
	base := strings.TrimSuffix(modelPath, ext(modelPath))
	for _, candidate := range []string{base + ".whe", base + ".WHE"} {
		if data, err := tree.ReadFile(candidate); err == nil {
			return data, nil
		}
	}
	if file := tree.FindByName(strings.ToLower(pathBase(base) + ".whe")); file != nil {
		return tree.ReadTreeFile(file)
	}
	return nil, nil
}

func pathBase(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[i+1:]
	}
	return name
}

func ext(name string) string {
	base := pathBase(name)
	if i := strings.LastIndexByte(base, '.'); i >= 0 {
		return base[i:]
	}
	return ""
}
