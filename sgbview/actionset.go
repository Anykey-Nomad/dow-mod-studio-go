package sgbview

import (
	"dow-mod-studio/internal/whe"
)

// ActionSet — действия .whe, которые сцена проигрывает как сценарий, а не
// как состояние анимации: телепорт (jump_setup/jump_breakdown), способности.
// В отличие от Anim, набор живёт и у моделей с запечённой позой (монолит):
// у них нельзя двигать кости, но действия всё равно задают клипы, эффекты и
// звуки, которые надо сыграть по таймлайнам.
type ActionSet struct {
	doc *whe.Document
}

// NewActionSet собирает набор действий по разобранному .whe.
func NewActionSet(doc *whe.Document) *ActionSet {
	if doc == nil {
		return nil
	}
	return &ActionSet{doc: doc}
}

// Clips — клипы, к которым ведёт действие: имена можно ставить через
// SetObjectFrames, даже если у модели нет обычной анимации.
func (s *ActionSet) Clips(action string) []string {
	if s == nil || s.doc == nil {
		return nil
	}
	return wheActionClips(s.doc, nil, action)
}

// Effects — эффекты таймлайнов действия с их временем от начала клипа.
func (s *ActionSet) Effects(action string) []FXEvent {
	if s == nil || s.doc == nil {
		return nil
	}
	return fxEventsForClips(s.doc, s.Clips(action))
}

// Sounds — звуки таймлайнов действия с их временем от начала клипа.
func (s *ActionSet) Sounds(action string) []SoundEvent {
	if s == nil || s.doc == nil {
		return nil
	}
	return fxSoundsForClips(s.doc, s.Clips(action))
}
