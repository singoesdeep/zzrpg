package quests

import (
	"context"
	"errors"
	"time"
)

var (
	ErrQuestNotFound         = errors.New("quest definition not found")
	ErrQuestAlreadyActive    = errors.New("quest is already active or completed")
	ErrQuestAlreadyCompleted = errors.New("quest is already completed")
	ErrLevelRequirement      = errors.New("character level is too low for this quest")
	ErrStepIndexOutOfBounds  = errors.New("quest step index out of bounds")
)

// Character-quest status values.
const (
	StatusActive    = "ACTIVE"
	StatusCompleted = "COMPLETED"
)

type QuestStep struct {
	Type   string `json:"type"`   // "KILL_MOB", "TALK_NPC", "COLLECT_ITEM"
	Target string `json:"target"` // e.g. "wolf", "blacksmith", "iron_ore"
	Count  int32  `json:"count"`  // count target required
}

type RewardItem struct {
	ItemDefinitionID string `json:"item_definition_id"`
	Quantity         int32  `json:"quantity"`
}

type QuestRewards struct {
	Experience int64        `json:"experience"`
	Gold       int64        `json:"gold"`
	Items      []RewardItem `json:"items"`
}

type QuestDefinition struct {
	ID          string         `json:"id"`
	Title       string         `json:"title"`
	Description string         `json:"description"`
	MinLevel    int32          `json:"min_level"`
	Steps       []QuestStep    `json:"steps"`
	Rewards     QuestRewards   `json:"rewards"`
	Metadata    map[string]any `json:"metadata"`
	CreatedAt   time.Time      `json:"created_at"`
}

type CharacterQuest struct {
	CharacterID      int32           `json:"character_id"`
	QuestID          string          `json:"quest_id"`
	Status           string          `json:"status"` // "ACTIVE", "COMPLETED"
	CurrentStepIndex int32           `json:"current_step_index"`
	Progress         []int32         `json:"progress"` // tracks progress value for each step
	UpdatedAt        time.Time       `json:"updated_at"`
	Definition       *QuestDefinition `json:"definition,omitempty"`
}

type QuestRepository interface {
	CreateDefinition(ctx context.Context, q *QuestDefinition) error
	GetDefinition(ctx context.Context, id string) (*QuestDefinition, error)
	ListDefinitions(ctx context.Context, limit, offset int) ([]QuestDefinition, error)
	AcceptQuest(ctx context.Context, charID int32, questID string, initialProgress []int32) error
	GetCharacterQuest(ctx context.Context, charID int32, questID string) (*CharacterQuest, error)
	ListCharacterQuests(ctx context.Context, charID int32) ([]CharacterQuest, error)
	UpdateProgress(ctx context.Context, charID int32, questID string, currentStep int32, progress []int32) error
	CompleteQuest(ctx context.Context, charID int32, questID string) error
}
