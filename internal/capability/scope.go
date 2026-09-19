package capability

import (
	"fmt"
	"github.com/faizahmd2/diagnos/internal/contract"
)

// ValidateScope enforces exact referential integrity for model-selected scopes.
func ValidateScope(cap Capability, scope contract.Entity, inv *contract.Investigation) error {
	if scope.Kind != cap.Accepts {
		return fmt.Errorf("scope rejected: %s accepts %s, got %s", cap.ID, cap.Accepts, scope.Kind)
	}
	if inv == nil {
		return fmt.Errorf("scope rejected: investigation is nil")
	}
	if cap.Level == contract.L1Machine && scope.Kind == contract.EntityMachine && scope.ID == "machine" {
		return nil
	}
	if !inv.KnowsEntity(scope) {
		return fmt.Errorf("scope rejected: %s/%s was not observed by a prior capability", scope.Kind, scope.ID)
	}
	return nil
}
