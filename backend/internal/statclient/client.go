package statclient

import (
	"context"
	"fmt"

	"github.com/singoesdeep/zzrpg/backend/internal/statclient/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type CharacterState struct {
	CharacterID int32
	BaseStats   map[string]float64
	Equipment   []Modifier
	Skills      []Modifier
	ActiveBuffs []Modifier
}

type Modifier struct {
	Stat      string
	Operation string
	Value     float64
	Priority  int32
	SourceID  string
}

type CombatStats struct {
	Level           int32
	Attack          float64
	Defense         float64
	Dex             float64
	CritRate        float64
	CritDamageBonus float64
	AccModifiers    float64
	DodgeModifiers  float64
}

type CalculateDamageReq struct {
	Attacker        CombatStats
	Defender        CombatStats
	SkillMultiplier float64
	SkillFlatDamage float64
}

type DamageResult struct {
	IsHit  bool
	Damage int32
	IsCrit bool
}

type Client interface {
	Calculate(ctx context.Context, state CharacterState) (map[string]float64, error)
	CalculateDamage(ctx context.Context, req CalculateDamageReq) (DamageResult, error)
	Close() error
}

type grpcStatClient struct {
	conn         *grpc.ClientConn
	grpcClient   pb.StatServiceClient
	combatClient pb.CombatServiceClient
}

func NewClient(addr string) (Client, error) {
	conn, err := grpc.Dial(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to connect to stat service at %s: %w", addr, err)
	}

	grpcClient := pb.NewStatServiceClient(conn)
	combatClient := pb.NewCombatServiceClient(conn)
	return &grpcStatClient{
		conn:         conn,
		grpcClient:   grpcClient,
		combatClient: combatClient,
	}, nil
}

func (c *grpcStatClient) Calculate(ctx context.Context, state CharacterState) (map[string]float64, error) {
	var pbModifiers []*pb.StatModifier

	// 1. Add base stats as modifiers (priority 10, source "base")
	for stat, val := range state.BaseStats {
		pbModifiers = append(pbModifiers, &pb.StatModifier{
			Stat:      stat,
			Operation: "ADD",
			Value:     val,
			Priority:  10,
			Source:    "base",
			SourceId:  "base_stat",
		})
	}

	// 2. Add equipment modifiers
	for _, m := range state.Equipment {
		pbModifiers = append(pbModifiers, &pb.StatModifier{
			Stat:      m.Stat,
			Operation: m.Operation,
			Value:     m.Value,
			Priority:  m.Priority,
			Source:    "equipment",
			SourceId:  m.SourceID,
		})
	}

	// 3. Add skills modifiers
	for _, m := range state.Skills {
		pbModifiers = append(pbModifiers, &pb.StatModifier{
			Stat:      m.Stat,
			Operation: m.Operation,
			Value:     m.Value,
			Priority:  m.Priority,
			Source:    "skill",
			SourceId:  m.SourceID,
		})
	}

	// 4. Add active buffs modifiers
	for _, m := range state.ActiveBuffs {
		pbModifiers = append(pbModifiers, &pb.StatModifier{
			Stat:      m.Stat,
			Operation: m.Operation,
			Value:     m.Value,
			Priority:  m.Priority,
			Source:    "buff",
			SourceId:  m.SourceID,
		})
	}

	req := &pb.CalculateStatsRequest{
		CharacterId: state.CharacterID,
		Modifiers:   pbModifiers,
	}

	resp, err := c.grpcClient.CalculateStats(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("grpc calculate stats failed: %w", err)
	}

	return resp.FinalStats, nil
}

func (c *grpcStatClient) CalculateDamage(ctx context.Context, req CalculateDamageReq) (DamageResult, error) {
	pbReq := &pb.CalculateDamageRequest{
		Attacker: &pb.CombatStats{
			Level:           req.Attacker.Level,
			Attack:          req.Attacker.Attack,
			Defense:         req.Attacker.Defense,
			Dex:             req.Attacker.Dex,
			CritRate:        req.Attacker.CritRate,
			CritDamageBonus: req.Attacker.CritDamageBonus,
			AccModifiers:    req.Attacker.AccModifiers,
			DodgeModifiers:  req.Attacker.DodgeModifiers,
		},
		Defender: &pb.CombatStats{
			Level:           req.Defender.Level,
			Attack:          req.Defender.Attack,
			Defense:         req.Defender.Defense,
			Dex:             req.Defender.Dex,
			CritRate:        req.Defender.CritRate,
			CritDamageBonus: req.Defender.CritDamageBonus,
			AccModifiers:    req.Defender.AccModifiers,
			DodgeModifiers:  req.Defender.DodgeModifiers,
		},
		SkillMultiplier: req.SkillMultiplier,
		SkillFlatDamage: req.SkillFlatDamage,
	}

	resp, err := c.combatClient.CalculateDamage(ctx, pbReq)
	if err != nil {
		return DamageResult{}, fmt.Errorf("grpc calculate damage failed: %w", err)
	}

	return DamageResult{
		IsHit:  resp.IsHit,
		Damage: resp.Damage,
		IsCrit: resp.IsCrit,
	}, nil
}

func (c *grpcStatClient) Close() error {
	return c.conn.Close()
}
