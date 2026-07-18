package character

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type pgCharacterRepository struct {
	pool *pgxpool.Pool
}

func NewCharacterRepository(pool *pgxpool.Pool) CharacterRepository {
	return &pgCharacterRepository{pool: pool}
}

func (r *pgCharacterRepository) Create(ctx context.Context, char *Character, baseStats map[string]float64) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Check character limit (max 4 per user)
	var count int
	err = tx.QueryRow(ctx, "SELECT COUNT(*) FROM characters WHERE user_id = $1", char.UserID).Scan(&count)
	if err != nil {
		return err
	}
	if count >= 4 {
		return ErrCharacterLimitReached
	}

	// 1. Insert character
	queryChar := `
		INSERT INTO characters (user_id, name, class_name)
		VALUES ($1, $2, $3)
		RETURNING id, level, experience, gold, last_active_at, created_at, updated_at
	`
	err = tx.QueryRow(ctx, queryChar, char.UserID, char.Name, char.ClassName).
		Scan(&char.ID, &char.Level, &char.Experience, &char.Gold, &char.LastActiveAt, &char.CreatedAt, &char.UpdatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // name unique violation
			return ErrCharacterNameTaken
		}
		return err
	}

	// 2. Insert initial base stats and empty derived stats
	baseJSON, err := json.Marshal(baseStats)
	if err != nil {
		return err
	}

	// Local calculation fallback for initial derived stats
	derivedStats := map[string]float64{
		"HP":        baseStats["CON"] * 15,
		"MP":        baseStats["INT"] * 10,
		"ATTACK":    baseStats["STR"] * 2,
		"DEFENSE":   baseStats["CON"] * 1,
		"CRIT_RATE": 5, // 5% default
	}
	derivedJSON, err := json.Marshal(derivedStats)
	if err != nil {
		return err
	}

	queryStats := `
		INSERT INTO character_stats (character_id, base_stats, derived_stats)
		VALUES ($1, $2, $3)
	`
	_, err = tx.Exec(ctx, queryStats, char.ID, baseJSON, derivedJSON)
	if err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func (r *pgCharacterRepository) GetByID(ctx context.Context, id int64) (*CharacterWithStats, error) {
	query := `
		SELECT c.id, c.user_id, c.name, c.class_name, c.level, c.experience, c.gold, c.last_active_at, c.created_at, c.updated_at,
		       s.base_stats, s.derived_stats, s.updated_at
		FROM characters c
		JOIN character_stats s ON c.id = s.character_id
		WHERE c.id = $1
	`
	var cws CharacterWithStats
	var baseBytes, derivedBytes []byte

	err := r.pool.QueryRow(ctx, query, id).Scan(
		&cws.ID, &cws.UserID, &cws.Name, &cws.ClassName, &cws.Level, &cws.Experience, &cws.Gold, &cws.LastActiveAt, &cws.CreatedAt, &cws.UpdatedAt,
		&baseBytes, &derivedBytes, &cws.Stats.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCharacterNotFound
		}
		return nil, err
	}

	cws.Stats.CharacterID = cws.ID
	if err := json.Unmarshal(baseBytes, &cws.Stats.BaseStats); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(derivedBytes, &cws.Stats.DerivedStats); err != nil {
		return nil, err
	}

	return &cws, nil
}

func (r *pgCharacterRepository) GetByName(ctx context.Context, name string) (*Character, error) {
	query := `
		SELECT id, user_id, name, class_name, level, experience, gold, last_active_at, created_at, updated_at
		FROM characters
		WHERE name = $1
	`
	var c Character
	err := r.pool.QueryRow(ctx, query, name).Scan(
		&c.ID, &c.UserID, &c.Name, &c.ClassName, &c.Level, &c.Experience, &c.Gold, &c.LastActiveAt, &c.CreatedAt, &c.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrCharacterNotFound
		}
		return nil, err
	}
	return &c, nil
}

func (r *pgCharacterRepository) ListByUserID(ctx context.Context, userID int64) ([]Character, error) {
	query := `
		SELECT id, user_id, name, class_name, level, experience, gold, last_active_at, created_at, updated_at
		FROM characters
		WHERE user_id = $1
		ORDER BY id ASC
	`
	rows, err := r.pool.Query(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chars []Character
	for rows.Next() {
		var c Character
		err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.ClassName, &c.Level, &c.Experience, &c.Gold, &c.LastActiveAt, &c.CreatedAt, &c.UpdatedAt)
		if err != nil {
			return nil, err
		}
		chars = append(chars, c)
	}

	return chars, nil
}

func (r *pgCharacterRepository) UpdateStats(ctx context.Context, charID int64, derivedStats map[string]float64) error {
	derivedJSON, err := json.Marshal(derivedStats)
	if err != nil {
		return err
	}

	query := `
		UPDATE character_stats
		SET derived_stats = $1, updated_at = $2
		WHERE character_id = $3
	`
	res, err := r.pool.Exec(ctx, query, derivedJSON, time.Now(), charID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrCharacterNotFound
	}
	return nil
}

func (r *pgCharacterRepository) UpdateLastActive(ctx context.Context, charID int64) error {
	query := `
		UPDATE characters
		SET last_active_at = $1
		WHERE id = $2
	`
	res, err := r.pool.Exec(ctx, query, time.Now(), charID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrCharacterNotFound
	}
	return nil
}

func (r *pgCharacterRepository) AddRewards(ctx context.Context, charID int64, goldToAdd int64, expToAdd int64) (bool, int32, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return false, 0, err
	}
	defer tx.Rollback(ctx)

	// Fetch current level, experience, gold
	var level int32
	var experience, gold int64
	err = tx.QueryRow(ctx, "SELECT level, experience, gold FROM characters WHERE id = $1 FOR UPDATE", charID).
		Scan(&level, &experience, &gold)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, 0, ErrCharacterNotFound
		}
		return false, 0, err
	}

	newGold := gold + goldToAdd
	newExp := experience + expToAdd
	newLevel := level
	leveledUp := false

	// Simple level-up algorithm: level N requires N * N * 100 EXP
	for {
		reqExp := int64(newLevel) * int64(newLevel) * 100
		if newExp >= reqExp {
			newExp -= reqExp
			newLevel++
			leveledUp = true
		} else {
			break
		}
	}

	// Update characters table
	_, err = tx.Exec(ctx, `
		UPDATE characters
		SET level = $1, experience = $2, gold = $3, updated_at = NOW()
		WHERE id = $4
	`, newLevel, newExp, newGold, charID)
	if err != nil {
		return false, 0, err
	}

	if leveledUp {
		// Fetch current base stats
		var baseBytes []byte
		err = tx.QueryRow(ctx, "SELECT base_stats FROM character_stats WHERE character_id = $1 FOR UPDATE", charID).Scan(&baseBytes)
		if err != nil {
			return false, 0, err
		}

		var baseStats map[string]float64
		if err := json.Unmarshal(baseBytes, &baseStats); err != nil {
			return false, 0, err
		}

		// Stat gains: +2 STR, +2 INT, +2 DEX, +2 CON for each level gained
		lvlsGained := newLevel - level
		baseStats["STR"] += float64(lvlsGained * 2)
		baseStats["INT"] += float64(lvlsGained * 2)
		baseStats["DEX"] += float64(lvlsGained * 2)
		baseStats["CON"] += float64(lvlsGained * 2)

		newBaseBytes, err := json.Marshal(baseStats)
		if err != nil {
			return false, 0, err
		}

		_, err = tx.Exec(ctx, `
			UPDATE character_stats
			SET base_stats = $1, updated_at = NOW()
			WHERE character_id = $2
		`, newBaseBytes, charID)
		if err != nil {
			return false, 0, err
		}
	}

	err = tx.Commit(ctx)
	if err != nil {
		return false, 0, err
	}

	return leveledUp, newLevel, nil
}
