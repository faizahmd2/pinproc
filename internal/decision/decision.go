package decision

import (
	"context"
)

// QuestionType defines the bounded judgment primitive.
type QuestionType string

const (
	QNoul   QuestionType = "noul"
	QChoice QuestionType = "choice"
	QScore  QuestionType = "score"
)

// Question is one bounded model question.
type Question struct {
	Type         QuestionType
	Instructions string
	Criteria     map[string]string
	Levels       []string
}

// Answer is a normalized model answer.
type Answer struct {
	Type          QuestionType
	Noul          float64
	Choice        string
	Score         float64
	Confidence    float64
	Probabilities map[string]float64
	Legend        map[string]string

// Provider is the AI decision boundary.
type Provider interface {
	Name() string
	Ask(context.Context, any, map[string]Question) (map[string]Answer, error)
}
