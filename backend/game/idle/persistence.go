package idle

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

// AssignmentRepo persists a character's single active idle focus.
type AssignmentRepo interface {
	// Get returns the character's assignment; ok is false when none is set.
	Get(ctx context.Context, charID int64) (a Assignment, ok bool, err error)
	// Set stores (or replaces) the character's assignment.
	Set(ctx context.Context, charID int64, a Assignment) error
}

// LifeskillState is a character's progress in one lifeskill.
type LifeskillState struct {
	SkillID string
	Level   int32
	XP      int64
}

// LifeskillRepo persists per-character lifeskill levels/xp.
type LifeskillRepo interface {
	// Get returns the character's state for a skill, defaulting to level 1 / 0 xp
	// when the character has never trained it.
	Get(ctx context.Context, charID int64, skillID string) (LifeskillState, error)
	// Upsert writes the skill's level and xp.
	Upsert(ctx context.Context, charID int64, s LifeskillState) error
}

// BuildingRepo persists per-character RTS building (generator) levels.
type BuildingRepo interface {
	// Levels returns generator_id -> level for a character (absent = 0).
	Levels(ctx context.Context, charID int64) (map[string]int32, error)
	// Get returns a single generator's level (0 when not built).
	Get(ctx context.Context, charID int64, generatorID string) (int32, error)
	// Set writes a generator's level.
	Set(ctx context.Context, charID int64, generatorID string, level int32) error
}

// WalletRepo persists a character's fungible resource balances.
type WalletRepo interface {
	// Balances returns resource_id -> amount for a character.
	Balances(ctx context.Context, charID int64) (map[string]int64, error)
	// Credit adds amount (which may be negative) to a resource balance.
	Credit(ctx context.Context, charID int64, resourceID string, amount int64) error
}

// --- pgx implementations -----------------------------------------------------

type pgAssignmentRepo struct{ db store.Store }
type pgLifeskillRepo struct{ db store.Store }
type pgBuildingRepo struct{ db store.Store }
type pgWalletRepo struct{ db store.Store }

// NewAssignmentRepo returns a Postgres-backed AssignmentRepo.
func NewAssignmentRepo(db store.Store) AssignmentRepo { return &pgAssignmentRepo{db: db} }

// NewLifeskillRepo returns a Postgres-backed LifeskillRepo.
func NewLifeskillRepo(db store.Store) LifeskillRepo { return &pgLifeskillRepo{db: db} }

// NewBuildingRepo returns a Postgres-backed BuildingRepo.
func NewBuildingRepo(db store.Store) BuildingRepo { return &pgBuildingRepo{db: db} }

// NewWalletRepo returns a Postgres-backed WalletRepo.
func NewWalletRepo(db store.Store) WalletRepo { return &pgWalletRepo{db: db} }

func (r *pgAssignmentRepo) Get(ctx context.Context, charID int64) (Assignment, bool, error) {
	var a Assignment
	err := r.db.QueryRow(ctx,
		`SELECT activity_type, activity_id FROM character_idle_assignment WHERE character_id = $1`,
		charID).Scan(&a.Type, &a.ID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Assignment{}, false, nil
		}
		return Assignment{}, false, err
	}
	return a, true, nil
}

func (r *pgAssignmentRepo) Set(ctx context.Context, charID int64, a Assignment) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO character_idle_assignment (character_id, activity_type, activity_id, assigned_at)
		VALUES ($1, $2, $3, CURRENT_TIMESTAMP)
		ON CONFLICT (character_id)
		DO UPDATE SET activity_type = EXCLUDED.activity_type,
		              activity_id   = EXCLUDED.activity_id,
		              assigned_at   = CURRENT_TIMESTAMP`,
		charID, string(a.Type), a.ID)
	return err
}

func (r *pgLifeskillRepo) Get(ctx context.Context, charID int64, skillID string) (LifeskillState, error) {
	s := LifeskillState{SkillID: skillID, Level: 1}
	err := r.db.QueryRow(ctx,
		`SELECT level, xp FROM character_lifeskills WHERE character_id = $1 AND skill_id = $2`,
		charID, skillID).Scan(&s.Level, &s.XP)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LifeskillState{SkillID: skillID, Level: 1, XP: 0}, nil
		}
		return LifeskillState{}, err
	}
	return s, nil
}

func (r *pgLifeskillRepo) Upsert(ctx context.Context, charID int64, s LifeskillState) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO character_lifeskills (character_id, skill_id, level, xp)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (character_id, skill_id)
		DO UPDATE SET level = EXCLUDED.level, xp = EXCLUDED.xp`,
		charID, s.SkillID, s.Level, s.XP)
	return err
}

func (r *pgBuildingRepo) Levels(ctx context.Context, charID int64) (map[string]int32, error) {
	rows, err := r.db.Query(ctx,
		`SELECT generator_id, level FROM character_buildings WHERE character_id = $1`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int32)
	for rows.Next() {
		var id string
		var lvl int32
		if err := rows.Scan(&id, &lvl); err != nil {
			return nil, err
		}
		out[id] = lvl
	}
	return out, rows.Err()
}

func (r *pgBuildingRepo) Get(ctx context.Context, charID int64, generatorID string) (int32, error) {
	var lvl int32
	err := r.db.QueryRow(ctx,
		`SELECT level FROM character_buildings WHERE character_id = $1 AND generator_id = $2`,
		charID, generatorID).Scan(&lvl)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return lvl, nil
}

func (r *pgBuildingRepo) Set(ctx context.Context, charID int64, generatorID string, level int32) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO character_buildings (character_id, generator_id, level)
		VALUES ($1, $2, $3)
		ON CONFLICT (character_id, generator_id)
		DO UPDATE SET level = EXCLUDED.level`,
		charID, generatorID, level)
	return err
}

func (r *pgWalletRepo) Balances(ctx context.Context, charID int64) (map[string]int64, error) {
	rows, err := r.db.Query(ctx,
		`SELECT resource_id, amount FROM character_resources WHERE character_id = $1`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make(map[string]int64)
	for rows.Next() {
		var id string
		var amt int64
		if err := rows.Scan(&id, &amt); err != nil {
			return nil, err
		}
		out[id] = amt
	}
	return out, rows.Err()
}

func (r *pgWalletRepo) Credit(ctx context.Context, charID int64, resourceID string, amount int64) error {
	_, err := r.db.Exec(ctx, `
		INSERT INTO character_resources (character_id, resource_id, amount)
		VALUES ($1, $2, $3)
		ON CONFLICT (character_id, resource_id)
		DO UPDATE SET amount = character_resources.amount + EXCLUDED.amount`,
		charID, resourceID, amount)
	return err
}
