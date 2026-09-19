package narrator

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
)

var tokenRE = regexp.MustCompile("[A-Za-z0-9_./:-]+")

// Validate rejects narration that introduces a number or identifier not present in the investigation.
func Validate(inv *contract.Investigation, text string) error {
	if inv == nil {
		return fmt.Errorf("investigation is nil")
	}
	if strings.TrimSpace(text) == "" {
		return nil
	}
	allowed := make(map[string]bool)
	add := func(s string) {
		for _, token := range tokenRE.FindAllString(s, -1) {
			allowed[token] = true
		}
	}
	b, _ := inv.JSON()
	add(string(b))
	for _, e := range inv.Evidence {
		add(e.Entity.ID)
		add(e.Entity.Display)
		for _, s := range e.Sources {
			add(s)
		}
	}
	for _, h := range inv.Hypotheses {
		add(h.Statement)
	}
	for _, token := range tokenRE.FindAllString(text, -1) {
		if looksSensitive(token) && !allowed[token] {
			return fmt.Errorf("narration introduced unsupported token %q", token)
		}
	}
	return nil
}

func looksSensitive(token string) bool {
	for _, r := range token {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return strings.Contains(token, "/") || strings.Contains(token, ":") || strings.Contains(token, ".")
}
