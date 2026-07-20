package character

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/singoesdeep/zzrpg/backend/engine/eventlog"
	"github.com/singoesdeep/zzrpg/backend/engine/outbox"
	"github.com/singoesdeep/zzrpg/backend/engine/store"
)

type pgCharacterRepository struct {
	db store.Store
}

func NewCharacterRepository(db store.Store) CharacterRepository {
	return &pgCharacterRepository{db: db}
}

func (r *pgCharacterRepository) Create(ctx context.Context, char *Character, baseStats, derivedStats map[string]float64) error {
	return r.db.WithinTx(ctx, func(q store.Querier) error {
		// Check character limit (max 4 per user)
		var count int
		err := q.QueryRow(ctx, "SELECT COUNT(*) FROM characters WHERE user_id = $1", char.UserID).Scan(&count)
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
		err = q.QueryRow(ctx, queryChar, char.UserID, char.Name, char.ClassName).
			Scan(&char.ID, &char.Level, &char.Experience, &char.Gold, &char.LastActiveAt, &char.CreatedAt, &char.UpdatedAt)
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" { // name unique violation
				return ErrCharacterNameTaken
			}
			return err
		}

		// 2. Insert base stats and the derived stats the caller computed (via
		// zzstat — persistence does no stat math).
		baseJSON, err := json.Marshal(baseStats)
		if err != nil {
			return err
		}
		derivedJSON, err := json.Marshal(derivedStats)
		if err != nil {
			return err
		}

		queryStats := `
			INSERT INTO character_stats (character_id, base_stats, derived_stats)
			VALUES ($1, $2, $3)
		`
		_, err = q.Exec(ctx, queryStats, char.ID, baseJSON, derivedJSON)
		return err
	})
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

	err := r.db.QueryRow(ctx, query, id).Scan(
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
	err := r.db.QueryRow(ctx, query, name).Scan(
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
	rows, err := r.db.Query(ctx, query, userID)
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
	res, err := r.db.Exec(ctx, query, derivedJSON, time.Now(), charID)
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
	res, err := r.db.Exec(ctx, query, time.Now(), charID)
	if err != nil {
		return err
	}
	if res.RowsAffected() == 0 {
		return ErrCharacterNotFound
	}
	return nil
}

func (r *pgCharacterRepository) AddRewards(ctx context.Context, charID int64, goldToAdd int64, expToAdd int64) (bool, int32, error) {
	var leveledUp bool
	var newLevel int32

	err := r.db.WithinTx(ctx, func(q store.Querier) error {
		// Fetch current level, experience, gold
		var level int32
		var experience, gold int64
		err := q.QueryRow(ctx, "SELECT level, experience, gold FROM characters WHERE id = $1 FOR UPDATE", charID).
			Scan(&level, &experience, &gold)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrCharacterNotFound
			}
			return err
		}

		newGold := gold + goldToAdd
		// Progression is a domain rule (see leveling.go), not persistence logic.
		var newExp int64
		newLevel, newExp, leveledUp = ApplyExperience(level, experience, expToAdd)

		// Update characters table
		_, err = q.Exec(ctx, `
			UPDATE characters
			SET level = $1, experience = $2, gold = $3, updated_at = NOW()
			WHERE id = $4
		`, newLevel, newExp, newGold, charID)
		if err != nil {
			return err
		}

		if leveledUp {
			// Fetch current base stats
			var baseBytes []byte
			err = q.QueryRow(ctx, "SELECT base_stats FROM character_stats WHERE character_id = $1 FOR UPDATE", charID).Scan(&baseBytes)
			if err != nil {
				return err
			}

			var baseStats map[string]float64
			if err := json.Unmarshal(baseBytes, &baseStats); err != nil {
				return err
			}

			// Apply per-level stat gains (domain rule, see leveling.go).
			ApplyLevelUpStatGains(baseStats, newLevel-level)

			newBaseBytes, err := json.Marshal(baseStats)
			if err != nil {
				return err
			}

			_, err = q.Exec(ctx, `
				UPDATE character_stats
				SET base_stats = $1, updated_at = NOW()
				WHERE character_id = $2
			`, newBaseBytes, charID)
			if err != nil {
				return err
			}
		}

		// Record the domain events in the SAME transaction as the state change,
		// so the reward (and any level-up) can never be lost or emitted without
		// the write actually committing. The outbox drives post-commit dispatch;
		// the event_log is the durable per-character history for replay.
		stream := eventlog.CharacterStream(charID)
		rewarded := RewardsGranted{CharacterID: charID, Gold: goldToAdd, Exp: expToAdd}
		if err := outbox.Append(ctx, q, rewarded); err != nil {
			return err
		}
		if err := eventlog.Append(ctx, q, stream, rewarded); err != nil {
			return err
		}
		if leveledUp {
			leveled := CharacterLeveledUp{CharacterID: charID, NewLevel: newLevel}
			if err := outbox.Append(ctx, q, leveled); err != nil {
				return err
			}
			if err := eventlog.Append(ctx, q, stream, leveled); err != nil {
				return err
			}
		}

		return nil
	})
	if err != nil {
		return false, 0, err
	}

	return leveledUp, newLevel, nil
}
