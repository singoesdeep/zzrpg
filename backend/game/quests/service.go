package quests

import (
	"context"

	"github.com/singoesdeep/zzrpg/backend/engine/bus"
	"github.com/singoesdeep/zzrpg/backend/engine/hooks"
	"github.com/singoesdeep/zzrpg/backend/game/character"
	"github.com/singoesdeep/zzrpg/backend/game/inventory"
)

type QuestService interface {
	CreateDefinition(ctx context.Context, q *QuestDefinition) error
	GetDefinition(ctx context.Context, id string) (*QuestDefinition, error)
	ListDefinitions(ctx context.Context, limit, offset int) ([]QuestDefinition, error)
	AcceptQuest(ctx context.Context, charID int32, questID string) error
	GetQuestLog(ctx context.Context, charID int32) ([]CharacterQuest, error)
	UpdateQuestProgress(ctx context.Context, charID int32, actionType string, target string, amount int32) error
}

// CharacterGateway is the minimal character-service surface quests needs: a level
// check on accept and reward crediting on completion. Consumer-owned so quests
// depends on the behaviour it uses, not the full character.CharacterService.
type CharacterGateway interface {
	GetByID(ctx context.Context, id int64) (*character.CharacterWithStats, error)
	AddRewards(ctx context.Context, charID int64, gold int64, exp int64) (bool, int32, error)
}

// InventoryWriter is the minimal inventory surface quests needs: granting reward
// items on quest completion.
type InventoryWriter interface {
	AddItem(ctx context.Context, item *inventory.InventoryItem) error
}

type questService struct {
	repo         QuestRepository
	charService  CharacterGateway
	inventorySvc InventoryWriter
	eventBus     bus.EventBus
	hooks        *hooks.Hooks
}

// NewQuestService builds the quest service. eventBus and hks may be nil (no
// domain events published / no hooks applied, respectively).
func NewQuestService(repo QuestRepository, charService CharacterGateway, inventorySvc InventoryWriter, eventBus bus.EventBus, hks *hooks.Hooks) QuestService {
	return &questService{
		repo:         repo,
		charService:  charService,
		inventorySvc: inventorySvc,
		eventBus:     eventBus,
		hooks:        hks,
	}
}

// publish emits ev on the bus when one is configured. Publishing is async and
// fire-and-forget, so it never affects the service's synchronous outcome.
func (s *questService) publish(ctx context.Context, ev bus.Event) {
	if s.eventBus != nil {
		_ = s.eventBus.Publish(ctx, ev)
	}
}

func (s *questService) CreateDefinition(ctx context.Context, q *QuestDefinition) error {
	return s.repo.CreateDefinition(ctx, q)
}

func (s *questService) GetDefinition(ctx context.Context, id string) (*QuestDefinition, error) {
	return s.repo.GetDefinition(ctx, id)
}

func (s *questService) ListDefinitions(ctx context.Context, limit, offset int) ([]QuestDefinition, error) {
	return s.repo.ListDefinitions(ctx, limit, offset)
}

func (s *questService) GetQuestLog(ctx context.Context, charID int32) ([]CharacterQuest, error) {
	return s.repo.ListCharacterQuests(ctx, charID)
}

func (s *questService) AcceptQuest(ctx context.Context, charID int32, questID string) error {
	// 1. Fetch quest definition
	qd, err := s.repo.GetDefinition(ctx, questID)
	if err != nil {
		return err
	}

	// 2. Fetch character details and level check
	char, err := s.charService.GetByID(ctx, int64(charID))
	if err != nil {
		return err
	}

	if char.Level < qd.MinLevel {
		return ErrLevelRequirement
	}

	// 2b. Let plugins gate quest acceptance (prerequisites, faction locks, event
	// windows). A returned error blocks the accept.
	if err := hooks.DoAction(s.hooks, ctx, HookAccept, QuestAccept{CharacterID: charID, QuestID: questID}); err != nil {
		return err
	}

	// 3. Verify if quest is already active or completed
	activeQuests, err := s.repo.ListCharacterQuests(ctx, charID)
	if err != nil {
		return err
	}

	for _, aq := range activeQuests {
		if aq.QuestID == questID {
			return ErrQuestAlreadyActive
		}
	}

	// 4. Initialize progress slice matching the steps length
	initialProgress := make([]int32, len(qd.Steps))

	// 5. Save to database
	if err := s.repo.AcceptQuest(ctx, charID, questID, initialProgress); err != nil {
		return err
	}

	s.publish(ctx, QuestAccepted{CharacterID: charID, QuestID: questID})
	return nil
}

func (s *questService) UpdateQuestProgress(ctx context.Context, charID int32, actionType string, target string, amount int32) error {
	// Let plugins scale the progress amount (e.g. a "double progress" event).
	amount = hooks.ApplyFilters(s.hooks, ctx, HookProgress, QuestProgressFilter{
		CharacterID: charID, ActionType: actionType, Target: target, Amount: amount,
	}).Amount

	// 1. Fetch active quests
	questsLog, err := s.repo.ListCharacterQuests(ctx, charID)
	if err != nil {
		return err
	}

	for _, cq := range questsLog {
		if cq.Status != StatusActive {
			continue
		}

		qd := cq.Definition
		if qd == nil {
			continue
		}

		currIdx := cq.CurrentStepIndex
		if currIdx < 0 || int(currIdx) >= len(qd.Steps) {
			continue
		}

		step := qd.Steps[currIdx]
		if step.Type == actionType && step.Target == target {
			// Update step progress
			currentProgress := cq.Progress[currIdx]
			if currentProgress >= step.Count {
				continue
			}

			newProgress := currentProgress + amount
			if newProgress > step.Count {
				newProgress = step.Count
			}

			cq.Progress[currIdx] = newProgress

			// Check step completion
			stepCompleted := newProgress >= step.Count
			isLastStep := int(currIdx) == len(qd.Steps)-1

			if stepCompleted && isLastStep {
				// Quest is fully completed!
				if err := s.repo.CompleteQuest(ctx, charID, cq.QuestID); err != nil {
					return err
				}

				// Award Rewards!
				rewards := qd.Rewards
				// Add EXP and Gold
				_, _, _ = s.charService.AddRewards(ctx, int64(charID), rewards.Gold, rewards.Experience)

				// Add Reward Items to inventory
				for _, rewardItem := range rewards.Items {
					invItem := &inventory.InventoryItem{
						CharacterID:      charID,
						ItemDefinitionID: rewardItem.ItemDefinitionID,
						Quantity:         rewardItem.Quantity,
						Durability:       100,
						CustomModifiers:  nil,
					}
					_ = s.inventorySvc.AddItem(ctx, invItem)
				}

				s.publish(ctx, QuestCompleted{CharacterID: charID, QuestID: cq.QuestID})
			} else if stepCompleted {
				// Move to next step
				cq.CurrentStepIndex++
				if err := s.repo.UpdateProgress(ctx, charID, cq.QuestID, cq.CurrentStepIndex, cq.Progress); err != nil {
					return err
				}
				s.publish(ctx, QuestProgressed{CharacterID: charID, QuestID: cq.QuestID, Step: cq.CurrentStepIndex})
			} else {
				// Just save progress
				if err := s.repo.UpdateProgress(ctx, charID, cq.QuestID, cq.CurrentStepIndex, cq.Progress); err != nil {
					return err
				}
				s.publish(ctx, QuestProgressed{CharacterID: charID, QuestID: cq.QuestID, Step: cq.CurrentStepIndex})
			}
		}
	}

	return nil
}
