package entity_test

import (
	"context"
	"errors"
	"testing"

	"github.com/singoesdeep/zzrpg/gamekit/entity"
)

func TestMemRepo(t *testing.T) {
	ctx := context.Background()
	r := entity.NewMemRepo()

	hero, err := r.Create(ctx, "character", 7)
	if err != nil || hero.ID == 0 || hero.Kind != "character" || hero.OwnerID != 7 {
		t.Fatalf("create hero: %+v err=%v", hero, err)
	}
	city, _ := r.Create(ctx, "city", 7)
	mob, _ := r.Create(ctx, "goblin", 0) // world-owned

	got, err := r.Get(ctx, hero.ID)
	if err != nil || got.ID != hero.ID {
		t.Fatalf("get: %+v err=%v", got, err)
	}

	owned, _ := r.ListByOwner(ctx, 7)
	if len(owned) != 2 {
		t.Fatalf("owner 7 should have 2 entities, got %d", len(owned))
	}
	goblins, _ := r.ListByKind(ctx, "goblin")
	if len(goblins) != 1 || goblins[0].ID != mob.ID {
		t.Fatalf("kind goblin wrong: %+v", goblins)
	}
	_ = city

	if err := r.Delete(ctx, hero.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := r.Get(ctx, hero.ID); !errors.As(err, &entity.ErrNotFound{}) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
}
