package engine

import (
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
)

func ShouldStop(now, start time.Time, spent contract.Spend, b contract.Budget) contract.StopReason {
	if b.MaxDepth > 0 && spent.Depth >= b.MaxDepth { return contract.StopBudgetDepth }
	if b.MaxSteps > 0 && spent.Steps >= b.MaxSteps { return contract.StopBudgetSteps }
	if b.MaxBytes > 0 && spent.Bytes >= b.MaxBytes { return contract.StopBudgetBytes }
	if b.MaxDecisionCalls > 0 && spent.DecisionCalls >= b.MaxDecisionCalls { return contract.StopBudgetSteps }
	if b.MaxNarrationCalls > 0 && spent.NarrationCalls >= b.MaxNarrationCalls { return contract.StopBudgetSteps }
	if b.MaxWall > 0 && now.Sub(start) >= b.MaxWall { return contract.StopBudgetTime }
	return ""
}