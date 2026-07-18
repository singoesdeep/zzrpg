package statclient

import (
	"context"
	"net"
	"testing"

	"github.com/singoesdeep/zzrpg/backend/internal/statclient/pb"
	"google.golang.org/grpc"
)

type mockStatServiceServer struct {
	pb.UnimplementedStatServiceServer
}

func (s *mockStatServiceServer) CalculateStats(ctx context.Context, req *pb.CalculateStatsRequest) (*pb.CalculateStatsResponse, error) {
	finalStats := map[string]float64{
		"HP":     5000,
		"ATTACK": 350,
	}

	return &pb.CalculateStatsResponse{
		FinalStats: finalStats,
	}, nil
}

type mockCombatServiceServer struct {
	pb.UnimplementedCombatServiceServer
}

func (s *mockCombatServiceServer) CalculateDamage(ctx context.Context, req *pb.CalculateDamageRequest) (*pb.CalculateDamageResponse, error) {
	return &pb.CalculateDamageResponse{
		IsHit:  true,
		Damage: 120,
		IsCrit: true,
	}, nil
}

func TestStatClient(t *testing.T) {
	// 1. Setup gRPC server
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatalf("failed to listen: %v", err)
	}
	defer listener.Close()

	grpcServer := grpc.NewServer()
	mockStat := &mockStatServiceServer{}
	mockCombat := &mockCombatServiceServer{}
	pb.RegisterStatServiceServer(grpcServer, mockStat)
	pb.RegisterCombatServiceServer(grpcServer, mockCombat)

	go func() {
		if err := grpcServer.Serve(listener); err != nil {
			// ignore server shutdown error
		}
	}()
	defer grpcServer.Stop()

	// 2. Setup Client
	addr := listener.Addr().String()
	client, err := NewClient(addr)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}
	defer client.Close()

	// 3. Test stats calculation
	state := CharacterState{
		CharacterID: 101,
		BaseStats:   map[string]float64{"STR": 10, "CON": 10},
		Equipment: []Modifier{
			{Stat: "ATTACK", Operation: "ADD", Value: 100, Priority: 20, SourceID: "sword_01"},
		},
	}

	result, err := client.Calculate(context.Background(), state)
	if err != nil {
		t.Fatalf("calculation failed: %v", err)
	}

	if result["HP"] != 5000 || result["ATTACK"] != 350 {
		t.Errorf("unexpected calculation result: %+v", result)
	}

	// 4. Test combat damage calculation
	combatReq := CalculateDamageReq{
		Attacker: CombatStats{
			Level:  10,
			Attack: 150,
		},
		Defender: CombatStats{
			Level:   10,
			Defense: 50,
		},
	}

	combatRes, err := client.CalculateDamage(context.Background(), combatReq)
	if err != nil {
		t.Fatalf("combat calculation failed: %v", err)
	}

	if !combatRes.IsHit || combatRes.Damage != 120 || !combatRes.IsCrit {
		t.Errorf("unexpected combat damage result: %+v", combatRes)
	}
}
