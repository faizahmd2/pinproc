package engine

import (
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func TestShouldStopEnforcesDecisionBudget(t *testing.T) {
	start := time.Now()
	b := contract.BudgetNormal()
	spent := contract.Spend{DecisionCalls: b.MaxDecisionCalls}
	got := ShouldStop(start, start, spent, b)
	if got != contract.StopBudgetDecisionCalls { t.Fatalf("got stop reason %q", got) }
}