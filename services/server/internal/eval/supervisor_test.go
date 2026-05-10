package eval

import (
	"strings"
	"testing"
	"time"

	"riverline_server/internal/models"
)

func TestStartSupervisorIsIdempotent(t *testing.T) {
	restore := installSupervisorTestHooks(t)
	defer restore()

	block := make(chan struct{})
	supervisorRunCycle = func(s *Supervisor, agentID models.AgentID, cfg SupervisorConfig) error {
		<-block
		return nil
	}

	if err := StartSupervisor(SupervisorConfig{AgentsRotation: []models.AgentID{models.AgentAria}, MaxRunDuration: time.Hour}); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	err := StartSupervisor(SupervisorConfig{AgentsRotation: []models.AgentID{models.AgentAria}, MaxRunDuration: time.Hour})
	if err == nil || !strings.Contains(err.Error(), "already running") {
		t.Fatalf("second start error = %v, want already running", err)
	}
	close(block)
	_ = StopSupervisor()
}

func TestStopSupervisorCancelsWithinTwoSeconds(t *testing.T) {
	restore := installSupervisorTestHooks(t)
	defer restore()

	if err := StartSupervisor(SupervisorConfig{AgentsRotation: []models.AgentID{models.AgentAria}, MaxRunDuration: time.Hour}); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	start := time.Now()
	if err := StopSupervisor(); err != nil {
		t.Fatalf("stop supervisor: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("stop took too long: %s", time.Since(start))
	}
}

func TestSupervisorStopsWhenBudgetExceeded(t *testing.T) {
	restore := installSupervisorTestHooks(t)
	defer restore()

	supervisorIncrementalSpentUSD = func(float64) (float64, error) {
		return 2, nil
	}
	if err := StartSupervisor(SupervisorConfig{AgentsRotation: []models.AgentID{models.AgentAria}, MaxIncrementalCostUSD: 1, MaxRunDuration: time.Hour}); err != nil {
		t.Fatalf("start supervisor: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status := CurrentSupervisorStatus()
		if !status.Running {
			if status.StopReason != "budget" {
				t.Fatalf("stop reason = %q, want budget", status.StopReason)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("supervisor did not stop after budget was exceeded")
}

func installSupervisorTestHooks(t *testing.T) func() {
	t.Helper()
	_ = StopSupervisor()
	learningSupervisor = &Supervisor{}
	oldTotal := supervisorTotalCostUSD
	oldIncremental := supervisorIncrementalSpentUSD
	oldRunCycle := supervisorRunCycle
	supervisorTotalCostUSD = func() (float64, error) { return 0, nil }
	supervisorIncrementalSpentUSD = func(float64) (float64, error) { return 0, nil }
	supervisorRunCycle = func(s *Supervisor, agentID models.AgentID, cfg SupervisorConfig) error {
		select {
		case <-s.ctx.Done():
			return nil
		case <-time.After(10 * time.Millisecond):
			return nil
		}
	}
	return func() {
		_ = StopSupervisor()
		learningSupervisor = &Supervisor{}
		supervisorTotalCostUSD = oldTotal
		supervisorIncrementalSpentUSD = oldIncremental
		supervisorRunCycle = oldRunCycle
	}
}
