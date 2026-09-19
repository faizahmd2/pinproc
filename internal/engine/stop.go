package engine

import (
	"github.com/faizahmd2/diagnos/internal/contract"
	"time"
)

// ShouldStop returns a budget stop reason, if any.
func ShouldStop(now, start time.Time, spent contract.Spend, b contract.Budget) contract.StopReason {
	if b.MaxDepth > 0 && spent.Depth >= b.MaxDepth {
		return contract.StopBudgetDepth
	}
	if b.MaxSteps > 0 && spent.Steps >= b.MaxSteps {
		return contract.StopBudgetSteps
	}
	if b.MaxBytes > 0 && spent.Bytes >= b.MaxBytes {
		return contract.StopBudgetBytes
	}
	if b.MaxWall > 0 && now.Sub(start) >= b.MaxWall {
		return contract.StopBudgetTime
	}
	return ""
}
