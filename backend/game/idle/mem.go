package idle

import (
	"context"
	"sync"
)

// In-memory implementations of the idle repositories. They back tests and can
// serve a DB-less/local mode; they are concurrency-safe.

type memAssignmentRepo struct {
	mu sync.Mutex
	m  map[int64]Assignment
}

// NewMemAssignmentRepo returns an in-memory AssignmentRepo.
func NewMemAssignmentRepo() AssignmentRepo { return &memAssignmentRepo{m: map[int64]Assignment{}} }

func (r *memAssignmentRepo) Get(_ context.Context, charID int64) (Assignment, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.m[charID]
	return a, ok, nil
}

func (r *memAssignmentRepo) Set(_ context.Context, charID int64, a Assignment) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[charID] = a
	return nil
}

type lsKey struct {
	char  int64
	skill string
}

type memLifeskillRepo struct {
	mu sync.Mutex
	m  map[lsKey]LifeskillState
}

// NewMemLifeskillRepo returns an in-memory LifeskillRepo.
func NewMemLifeskillRepo() LifeskillRepo { return &memLifeskillRepo{m: map[lsKey]LifeskillState{}} }

func (r *memLifeskillRepo) Get(_ context.Context, charID int64, skillID string) (LifeskillState, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.m[lsKey{charID, skillID}]; ok {
		return s, nil
	}
	return LifeskillState{SkillID: skillID, Level: 1}, nil
}

func (r *memLifeskillRepo) Upsert(_ context.Context, charID int64, s LifeskillState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.m[lsKey{charID, s.SkillID}] = s
	return nil
}

type memBuildingRepo struct {
	mu sync.Mutex
	m  map[int64]map[string]int32
}

// NewMemBuildingRepo returns an in-memory BuildingRepo.
func NewMemBuildingRepo() BuildingRepo { return &memBuildingRepo{m: map[int64]map[string]int32{}} }

func (r *memBuildingRepo) Levels(_ context.Context, charID int64) (map[string]int32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int32, len(r.m[charID]))
	for k, v := range r.m[charID] {
		out[k] = v
	}
	return out, nil
}

func (r *memBuildingRepo) Get(_ context.Context, charID int64, generatorID string) (int32, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.m[charID][generatorID], nil
}

func (r *memBuildingRepo) Set(_ context.Context, charID int64, generatorID string, level int32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m[charID] == nil {
		r.m[charID] = map[string]int32{}
	}
	r.m[charID][generatorID] = level
	return nil
}

type memWalletRepo struct {
	mu sync.Mutex
	m  map[int64]map[string]int64
}

// NewMemWalletRepo returns an in-memory WalletRepo.
func NewMemWalletRepo() WalletRepo { return &memWalletRepo{m: map[int64]map[string]int64{}} }

func (r *memWalletRepo) Balances(_ context.Context, charID int64) (map[string]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]int64, len(r.m[charID]))
	for k, v := range r.m[charID] {
		out[k] = v
	}
	return out, nil
}

func (r *memWalletRepo) Credit(_ context.Context, charID int64, resourceID string, amount int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m[charID] == nil {
		r.m[charID] = map[string]int64{}
	}
	r.m[charID][resourceID] += amount
	return nil
}
