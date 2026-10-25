package whe

import (
	"bytes"
	"strings"
	"testing"

	"dow-mod-studio/internal/chunky"
)

// restWhe собирает небольшой .whe с графом действий, устроенным как в
// поставляемых файлах: default ведёт в поддействия и мотионы, мотионы —
// через таймлайны — к клипам модели.
func restWhe(extraMotions ...FxActionLink) []byte {
	mton := func(name string, animations []string, random []RandomMotion) *chunky.Chunk {
		payload := (&Mton{Animations: animations, random: random}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 4, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	mt.Children = append(mt.Children,
		mton("vis_base", []string{"vis_base"}, nil),
		mton("vis_banner_1", []string{"vis_banner_01", "vis_banner_rod_01"}, nil),
		mton("idle_shared", nil, []RandomMotion{{Name: "idle_1", Weight: 1}}),
		mton("idle_1", []string{"idle_1"}, nil),
		mton("damage_50", []string{"vis_damage_50"}, nil),
	)
	acts := []FxAction{
		{Name: "default", Subactions: []FxActionLink{
			{Name: "vis_master", Compare: FxCompareNone},
			{Name: "damage_50", Compare: FxCompareIf, Condition: "damage_50"},
			{Name: "idle", Compare: FxCompareNone},
		}},
		{Name: "vis_master", Motions: append([]FxActionLink{
			{Name: "vis_banner_1", Compare: FxCompareNone},
			{Name: "vis_addon_1", Compare: FxCompareIf, Condition: "addon_1"},
			{Name: "vis_base", Compare: FxCompareElse},
		}, extraMotions...)},
		{Name: "damage_50", Motions: []FxActionLink{
			{Name: "damage_50", Compare: FxCompareNone},
		}},
		{Name: "idle", Motions: []FxActionLink{
			{Name: "idle_shared", Compare: FxCompareNone},
		}},
	}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Version: 1, Payload: MarshalActs(acts)},
		mt,
	}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	return buf.Bytes()
}

func TestRestClipNamesFollowsThePristineBranches(t *testing.T) {
	doc, err := ParseDocument(restWhe())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	got := doc.RestClipNames()
	want := map[string]bool{
		"vis_banner_01":     true,
		"vis_banner_rod_01": true,
		"vis_base":          true,
		"idle_1":            true,
	}
	if len(got) != len(want) {
		t.Fatalf("клипы = %v, want набор %v", got, want)
	}
	for _, name := range got {
		if !want[strings.ToLower(name)] {
			t.Errorf("лишний клип %q в %v", name, got)
		}
	}
}

// Безусловный мотион без таймлайна проходит дальше своим именем: настоящим
// клипом или нет — решает модель, а не граф.
func TestRestClipNamesCarriesMotionsWithoutTimelines(t *testing.T) {
	doc, err := ParseDocument(restWhe(FxActionLink{Name: "orphan_motion", Compare: FxCompareNone}))
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	found := false
	for _, name := range doc.RestClipNames() {
		if name == "orphan_motion" {
			found = true
		}
	}
	if !found {
		t.Error("мотион без таймлайна потерялся")
	}
}

// Цепочка мотионов может замыкаться: обход обязан заканчиваться.
func TestRestClipNamesSurvivesMotionCycles(t *testing.T) {
	mton := func(name string, motions []string) *chunky.Chunk {
		var random []RandomMotion
		for _, m := range motions {
			random = append(random, RandomMotion{Name: m, Weight: 1})
		}
		payload := (&Mton{Animations: []string{name + "_clip"}, random: random}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 4, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	mt.Children = append(mt.Children,
		mton("loop_a", []string{"loop_b"}),
		mton("loop_b", []string{"loop_a"}),
	)
	acts := []FxAction{
		{Name: "default", Motions: []FxActionLink{{Name: "loop_a", Compare: FxCompareNone}}},
	}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Version: 1, Payload: MarshalActs(acts)},
		mt,
	}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	doc, err := ParseDocument(buf.Bytes())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	if got := doc.RestClipNames(); len(got) != 2 {
		t.Fatalf("клипы = %v, want оба клипа из петли", got)
	}
}

// Действие idle ведёт в несколько мотионов, и каждый мотион — слой одной
// модели: таймлайн без случайных ссылок отдаёт все свои клипы (разные меши
// играют вместе), а таймлайн со взвешенными ссылками выбирает один вариант
// из нескольких — независимо для каждого мотиона.
func TestActionAnimationNamesCombinesLayersAndPicksOneVariantPerLayer(t *testing.T) {
	mton := func(name string, animations []string, random []RandomMotion) *chunky.Chunk {
		payload := (&Mton{Animations: animations, random: random}).MarshalBinary()
		return &chunky.Chunk{TypeID: "DATAMTON", Version: 4, Name: name, Payload: payload}
	}
	mt := &chunky.Chunk{TypeID: "FOLDMTRE", Version: 1}
	mt.Children = append(mt.Children,
		// Слой корпуса: один клип, без выбора.
		mton("idle_body", []string{"idle_body_clip"}, nil),
		// Слой кристаллов: два равновесных варианта, берётся один.
		mton("idle_crystal", nil, []RandomMotion{{Name: "idle_crystal_1", Weight: 1}, {Name: "idle_crystal_2", Weight: 1}}),
		mton("idle_crystal_1", []string{"idle_crystal_a"}, nil),
		mton("idle_crystal_2", []string{"idle_crystal_b"}, nil),
	)
	acts := []FxAction{{Name: "idle", Motions: []FxActionLink{
		{Name: "idle_body", Compare: FxCompareNone},
		{Name: "idle_crystal", Compare: FxCompareNone},
	}}}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Version: 1, Payload: MarshalActs(acts)},
		mt,
	}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	doc, err := ParseDocument(buf.Bytes())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	seenVariants := map[string]bool{}
	for seed := int64(0); seed < 32; seed++ {
		got := doc.ActionAnimationNamesWithSeed("idle", seed)
		if len(got) != 2 {
			t.Fatalf("seed %d: клипы = %v, want ровно два: слой корпуса и один вариант кристаллов", seed, got)
		}
		if got[0] != "idle_body_clip" {
			t.Fatalf("seed %d: слой корпуса = %q, want idle_body_clip", seed, got[0])
		}
		switch got[1] {
		case "idle_crystal_a", "idle_crystal_b":
			seenVariants[got[1]] = true
		default:
			t.Fatalf("seed %d: вариант кристаллов = %q, want один из двух своих", seed, got[1])
		}
	}
	if len(seenVariants) != 2 {
		t.Errorf("за 32 зерна выбрались варианты %v, want оба: выбор должен быть случайным", seenVariants)
	}
	// Действия без такого имени нечего разрешать.
	if got := doc.ActionAnimationNamesWithSeed("attack", 1); len(got) != 0 {
		t.Errorf("неизвестное действие = %v, want пусто", got)
	}
}

// Нет действия default — граф состояния не нашёлся, возвращать нечего.
func TestRestClipNamesWithoutDefaultActionIsEmpty(t *testing.T) {
	acts := []FxAction{
		{Name: "idle", Motions: []FxActionLink{{Name: "vis_base", Compare: FxCompareNone}}},
	}
	root := &chunky.Chunk{TypeID: "FOLDREBP", Version: 4, Children: []*chunky.Chunk{
		{TypeID: "DATAACTS", Version: 1, Payload: MarshalActs(acts)},
	}}
	var buf bytes.Buffer
	buf.Write(chunky.Preamble())
	buf.Write(chunky.Serialize(root))
	doc, err := ParseDocument(buf.Bytes())
	if err != nil {
		t.Fatalf("разбор .whe: %v", err)
	}
	if got := doc.RestClipNames(); len(got) != 0 {
		t.Fatalf("клипы = %v, want пусто без default", got)
	}
}
