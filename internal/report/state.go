package report

import (
	"encoding/json"
	"fmt"
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
	ID         string           `json:"id,omitempty"`
	Host       string           `json:"host,omitempty"`
	Trigger    string           `json:"trigger,omitempty"`
	Stage      string           `json:"stage,omitempty"`
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

func StartState(root, id, host, trigger string) error {
	now := time.Now()
	return WriteState(root, InspectionState{
		Status: StatusRunning, ID: id, Host: host, Trigger: trigger,
		Stage: "accepted", StartedAt: &now,
	})
}

func UpdateState(root, stage string) error {
	s, err := ReadState(root)
	if err != nil {
		return err
	}
	s.Stage = stage
	return WriteState(root, s)
}

func FinishState(root string) error {
	s, err := ReadState(root)
	if err != nil {
		return err
	}
	now := time.Now()
	s.Status = StatusDone
	s.Stage = "done"
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
	s.Stage = "failed"
	s.FinishedAt = &now
	s.Error = reason
	return WriteState(root, s)
}

func RecoverInterrupted(root string) error {
	s, err := ReadState(root)
	if err != nil || s.Status != StatusRunning {
		return nil
	}
	reason := "service restarted while an inspection was running"
	if s.ID != "" {
		reason = fmt.Sprintf("service restarted while inspection %s was running", s.ID)
	}
	return FailState(root, StatusInterrupted, reason)
}
