package report

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

type InspectionStatus string

const (
	StatusIdle        InspectionStatus = "idle"
	StatusRunning     InspectionStatus = "running"
	StatusDone        InspectionStatus = "done"
	StatusFailed      InspectionStatus = "failed"
	StatusInterrupted InspectionStatus = "interrupted"
)

type InspectionState struct {
	Status     InspectionStatus `json:"status"`
	StartedAt  *time.Time       `json:"started_at,omitempty"`
	UpdatedAt  time.Time        `json:"updated_at"`
	FinishedAt *time.Time       `json:"finished_at,omitempty"`
	Error      string           `json:"error,omitempty"`
}

func ReadState(root string) (InspectionState, error) {
	b, err := os.ReadFile(filepath.Join(root, "state.json"))
	if err != nil {
		return InspectionState{}, err
	}
	var s InspectionState
	if err := json.Unmarshal(b, &s); err != nil {
		return InspectionState{}, err
	}
	return s, nil
}

func WriteState(root string, s InspectionState) error {
	if s.Status == "" {
		s.Status = StatusIdle
	}
	s.UpdatedAt = time.Now()
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return atomic(filepath.Join(root, "state.json"), append(b, '\n'))
}

func StartState(root string, startedAt time.Time) error {
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	return WriteState(root, InspectionState{Status: StatusRunning, StartedAt: &startedAt})
}

func FinishState(root string) error {
	s, err := ReadState(root)
	if err != nil {
		return err
	}
	now := time.Now()
	s.Status = StatusDone
	s.FinishedAt = &now
	s.Error = ""
	return WriteState(root, s)
}

func FailState(root string, status InspectionStatus, reason string) error {
	if status != StatusFailed && status != StatusInterrupted {
		status = StatusFailed
	}
	s, err := ReadState(root)
	if err != nil {
		s = InspectionState{Status: status}
	}
	now := time.Now()
	s.Status = status
	s.FinishedAt = &now
	s.Error = reason
	return WriteState(root, s)
}
