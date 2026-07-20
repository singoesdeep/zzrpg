package quests

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

type pgQuestRepository struct {
	db store.Store
}

func NewQuestRepository(db store.Store) QuestRepository {
	return &pgQuestRepository{db: db}
}

func (r *pgQuestRepository) CreateDefinition(ctx context.Context, q *QuestDefinition) error {
	stepsJSON, err := json.Marshal(q.Steps)
	if err != nil {
		return err
	}
	rewardsJSON, err := json.Marshal(q.Rewards)
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(q.Metadata)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO quest_definitions (id, title, description, min_level, steps, rewards, metadata)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`
	return r.db.QueryRow(ctx, query, q.ID, q.Title, q.Description, q.MinLevel, stepsJSON, rewardsJSON, metaJSON).Scan(&q.CreatedAt)
}

func (r *pgQuestRepository) GetDefinition(ctx context.Context, id string) (*QuestDefinition, error) {
	query := `
		SELECT id, title, description, min_level, steps, rewards, metadata, created_at
		FROM quest_definitions
		WHERE id = $1
	`
	var q QuestDefinition
	var stepsBytes, rewardsBytes, metaBytes []byte

	err := r.db.QueryRow(ctx, query, id).Scan(
		&q.ID, &q.Title, &q.Description, &q.MinLevel, &stepsBytes, &rewardsBytes, &metaBytes, &q.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrQuestNotFound
		}
		return nil, err
	}

	if err := json.Unmarshal(stepsBytes, &q.Steps); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rewardsBytes, &q.Rewards); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metaBytes, &q.Metadata); err != nil {
		return nil, err
	}

	return &q, nil
}

func (r *pgQuestRepository) ListDefinitions(ctx context.Context, limit, offset int) ([]QuestDefinition, error) {
	query := `
		SELECT id, title, description, min_level, steps, rewards, metadata, created_at
		FROM quest_definitions
		ORDER BY id ASC
		LIMIT $1 OFFSET $2
	`
	rows, err := r.db.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []QuestDefinition
	for rows.Next() {
		var q QuestDefinition
		var stepsBytes, rewardsBytes, metaBytes []byte

		err := rows.Scan(
			&q.ID, &q.Title, &q.Description, &q.MinLevel, &stepsBytes, &rewardsBytes, &metaBytes, &q.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		if err := json.Unmarshal(stepsBytes, &q.Steps); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rewardsBytes, &q.Rewards); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metaBytes, &q.Metadata); err != nil {
			return nil, err
		}

		list = append(list, q)
	}

	return list, nil
}

func (r *pgQuestRepository) AcceptQuest(ctx context.Context, charID int32, questID string, initialProgress []int32) error {
	progJSON, err := json.Marshal(initialProgress)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO character_quests (character_id, quest_id, status, current_step_index, progress)
		VALUES ($1, $2, 'ACTIVE', 0, $3)
	`
	_, err = r.db.Exec(ctx, query, charID, questID, progJSON)
	return err
}

func (r *pgQuestRepository) GetCharacterQuest(ctx context.Context, charID int32, questID string) (*CharacterQuest, error) {
	query := `
		SELECT cq.character_id, cq.quest_id, cq.status, cq.current_step_index, cq.progress, cq.updated_at,
		       qd.title, qd.description, qd.min_level, qd.steps, qd.rewards, qd.metadata, qd.created_at
		FROM character_quests cq
		JOIN quest_definitions qd ON cq.quest_id = qd.id
		WHERE cq.character_id = $1 AND cq.quest_id = $2
	`
	var cq CharacterQuest
	cq.Definition = &QuestDefinition{}
	var progBytes, stepsBytes, rewardsBytes, metaBytes []byte

	err := r.db.QueryRow(ctx, query, charID, questID).Scan(
		&cq.CharacterID, &cq.QuestID, &cq.Status, &cq.CurrentStepIndex, &progBytes, &cq.UpdatedAt,
		&cq.Definition.Title, &cq.Definition.Description, &cq.Definition.MinLevel, &stepsBytes, &rewardsBytes, &metaBytes, &cq.Definition.CreatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrQuestNotFound
		}
		return nil, err
	}

	cq.Definition.ID = cq.QuestID
	if err := json.Unmarshal(progBytes, &cq.Progress); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(stepsBytes, &cq.Definition.Steps); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(rewardsBytes, &cq.Definition.Rewards); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(metaBytes, &cq.Definition.Metadata); err != nil {
		return nil, err
	}

	return &cq, nil
}

func (r *pgQuestRepository) ListCharacterQuests(ctx context.Context, charID int32) ([]CharacterQuest, error) {
	query := `
		SELECT cq.character_id, cq.quest_id, cq.status, cq.current_step_index, cq.progress, cq.updated_at,
		       qd.title, qd.description, qd.min_level, qd.steps, qd.rewards, qd.metadata, qd.created_at
		FROM character_quests cq
		JOIN quest_definitions qd ON cq.quest_id = qd.id
		WHERE cq.character_id = $1
		ORDER BY cq.updated_at DESC
	`
	rows, err := r.db.Query(ctx, query, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []CharacterQuest
	for rows.Next() {
		var cq CharacterQuest
		cq.Definition = &QuestDefinition{}
		var progBytes, stepsBytes, rewardsBytes, metaBytes []byte

		err := rows.Scan(
			&cq.CharacterID, &cq.QuestID, &cq.Status, &cq.CurrentStepIndex, &progBytes, &cq.UpdatedAt,
			&cq.Definition.Title, &cq.Definition.Description, &cq.Definition.MinLevel, &stepsBytes, &rewardsBytes, &metaBytes, &cq.Definition.CreatedAt,
		)
		if err != nil {
			return nil, err
		}

		cq.Definition.ID = cq.QuestID
		if err := json.Unmarshal(progBytes, &cq.Progress); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(stepsBytes, &cq.Definition.Steps); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(rewardsBytes, &cq.Definition.Rewards); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metaBytes, &cq.Definition.Metadata); err != nil {
			return nil, err
		}

		list = append(list, cq)
	}

	return list, nil
}

func (r *pgQuestRepository) UpdateProgress(ctx context.Context, charID int32, questID string, currentStep int32, progress []int32) error {
	progJSON, err := json.Marshal(progress)
	if err != nil {
		return err
	}

	query := `
		UPDATE character_quests
		SET current_step_index = $1, progress = $2, updated_at = NOW()
		WHERE character_id = $3 AND quest_id = $4 AND status = 'ACTIVE'
	`
	res, err := r.db.Exec(ctx, query, currentStep, progJSON, charID, questID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrQuestNotFound
	}
	return nil
}

func (r *pgQuestRepository) CompleteQuest(ctx context.Context, charID int32, questID string) error {
	query := `
		UPDATE character_quests
		SET status = 'COMPLETED', updated_at = NOW()
		WHERE character_id = $1 AND quest_id = $2 AND status = 'ACTIVE'
	`
	res, err := r.db.Exec(ctx, query, charID, questID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrQuestNotFound
	}
	return nil
}
