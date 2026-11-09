package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
)

// SeedPacks imports every pack template as a workflow (publishing v1 when the actor may publish)
// and applies pack evolutions, in order, as the next version: an evolution whose document says
// "status": "published" is published when the actor may publish, otherwise it stays a draft. It runs
// through the normal commands, so authorization, validation and audit apply. Re-running only adds
// evolutions whose version is exactly next and that have no open draft in their way.
func (s *Service) SeedPacks(ctx context.Context, a auth.Identity, ps []packs.Pack) ([]string, error) {
	var log []string
	templates, err := packs.Templates(ps)
	if err != nil {
		return nil, err
	}
	for _, t := range templates {
		_, err := s.GetDefinition(ctx, a, t.Key)
		if err == nil {
			log = append(log, fmt.Sprintf("%s: exists, skipped", t.Key))
			continue
		}
		if !errors.Is(err, ErrNotFound) {
			return log, err
		}
		res, err := s.CreateDefinition(ctx, a, Idempotency{Key: "seed:" + t.Key, Hash: RequestHash(t.Document)}, CreateInput{Key: t.Key, Template: t.Key})
		if err != nil {
			return log, fmt.Errorf("import %s: %w", t.Key, err)
		}
		if !a.HasRole(auth.RoleWorkflowPublisher) {
			log = append(log, fmt.Sprintf("%s: imported as draft v1 (no publisher role)", t.Key))
			continue
		}
		if _, err := s.Publish(ctx, a, t.Key, 1, res.Version.VersionNum); err != nil {
			return log, fmt.Errorf("publish %s: %w", t.Key, err)
		}
		log = append(log, fmt.Sprintf("%s: imported and published v1", t.Key))
	}

	evolutions, err := packs.Evolutions(ps)
	if err != nil {
		return log, err
	}
	for _, doc := range evolutions {
		var head struct {
			Key     string `json:"key"`
			Version int    `json:"version"`
			Status  string `json:"status"`
		}
		if err := json.Unmarshal(doc, &head); err != nil {
			return log, err
		}
		detail, err := s.GetDefinition(ctx, a, head.Key)
		if err != nil || detail.VersionCount >= head.Version || detail.OpenDraft != nil || detail.LatestPublished == nil {
			log = append(log, fmt.Sprintf("%s v%d: skipped", head.Key, head.Version))
			continue
		}
		draft, err := s.CreateDraftVersion(ctx, a, Idempotency{Key: fmt.Sprintf("seed:%s:v%d", head.Key, head.Version), Hash: RequestHash(doc)}, head.Key, 0)
		if err != nil {
			return log, fmt.Errorf("fork %s: %w", head.Key, err)
		}
		if draft.Version.Version != head.Version {
			log = append(log, fmt.Sprintf("%s: forked v%d, evolution expects v%d; left as fork", head.Key, draft.Version.Version, head.Version))
			continue
		}
		note := "Imported pack evolution example"
		updated, err := s.UpdateDraft(ctx, a, head.Key, head.Version, draft.Version.VersionNum, UpdateInput{Definition: doc, ChangeNote: &note})
		if err != nil {
			return log, fmt.Errorf("apply %s v%d: %w", head.Key, head.Version, err)
		}
		if head.Status != StatusPublished || !a.HasRole(auth.RoleWorkflowPublisher) {
			log = append(log, fmt.Sprintf("%s: draft v%d created", head.Key, head.Version))
			continue
		}
		if _, err := s.Publish(ctx, a, head.Key, head.Version, updated.Version.VersionNum); err != nil {
			return log, fmt.Errorf("publish %s v%d: %w", head.Key, head.Version, err)
		}
		log = append(log, fmt.Sprintf("%s: v%d imported and published", head.Key, head.Version))
	}
	return log, nil
}
