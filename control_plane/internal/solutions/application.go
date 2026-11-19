package solutions

import (
	"github.com/elenio/control-plane/internal/platform/apperr"
	"strings"
)

// Application is declarative navigation/composition, never executable HTML, scripts or URLs.
// Rendering a preview does not provision an app or grant permission to a referenced workflow.
type Application struct {
	Title string            `json:"title"`
	Pages []ApplicationPage `json:"pages"`
}
type ApplicationPage struct {
	Key         string `json:"key"`
	Title       string `json:"title"`
	Kind        string `json:"kind"` // overview | intake | tasks | report
	WorkflowKey string `json:"workflow_key,omitempty"`
}

func ValidateApplication(d Definition) error {
	if d.Application == nil {
		return nil
	} // Backward-compatible plans can remain incomplete.
	invalid := func() error {
		return apperr.BadRequest("INVALID_APPLICATION", "Use a 2–120 character app title, 1–12 uniquely keyed pages, supported components and linked workflow keys")
	}
	if d.Output == "api" || strings.TrimSpace(d.Application.Title) != d.Application.Title || len([]rune(d.Application.Title)) < 2 || len([]rune(d.Application.Title)) > 120 || len(d.Application.Pages) < 1 || len(d.Application.Pages) > 12 {
		return invalid()
	}
	refs := map[string]bool{}
	for _, w := range d.Workflows {
		refs[w.Key] = true
	}
	keys := map[string]bool{}
	for _, page := range d.Application.Pages {
		if !keyPattern.MatchString(page.Key) || keys[page.Key] || strings.TrimSpace(page.Title) != page.Title || len([]rune(page.Title)) < 2 || len([]rune(page.Title)) > 80 {
			return invalid()
		}
		keys[page.Key] = true
		switch page.Kind {
		case "overview", "intake", "tasks", "report":
		default:
			return invalid()
		}
		if (page.Kind != "overview" && page.WorkflowKey == "") || (page.WorkflowKey != "" && !refs[page.WorkflowKey]) {
			return invalid()
		}
	}
	return nil
}
