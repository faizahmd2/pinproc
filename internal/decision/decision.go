package decision

import (
	"context"
)

type QuestionType string

const (
	QNoul   QuestionType = "noul"
	QChoice QuestionType = "choice"
	QScore  QuestionType = "score"
)

type Question struct {
	Type         QuestionType      `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
	Levels       []string          `json:"levels,omitempty"`
}

type Answer struct {
	Type          QuestionType       `json:"type"`
	Noul          float64            `json:"noul,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

type Provider interface {
	Name() string
	Ask(context.Context, any, map[string]Question) (map[string]Answer, error)
}
