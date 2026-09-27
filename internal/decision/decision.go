package decision

import "context"

// QuestionType defines the bounded judgment primitive.
type QuestionType string

const (
	QBoolean QuestionType = "boolean"
	QChoice  QuestionType = "choice"
	QScore   QuestionType = "score"
)

// Question is one bounded model question.
type Question struct {
	Type         QuestionType      `json:"type"`
	Instructions string            `json:"instructions"`
	Criteria     map[string]string `json:"criteria,omitempty"`
	Levels       []string          `json:"levels,omitempty"`
}

// Answer is a normalized provider answer.
type Answer struct {
	Type          QuestionType       `json:"type"`
	Boolean       float64            `json:"boolean,omitempty"`
	Choice        string             `json:"choice,omitempty"`
	Score         float64            `json:"score,omitempty"`
	Confidence    float64            `json:"confidence,omitempty"`
	Probabilities map[string]float64 `json:"probabilities,omitempty"`
	Legend        map[string]string  `json:"legend,omitempty"`
}

// Provider is the AI decision boundary.
type Provider interface {
	Name() string
	Ask(context.Context, any, map[string]Question) (map[string]Answer, error)
}
