package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/elenio/flowcase/internal/audit"
	"github.com/elenio/flowcase/internal/packs"
	"github.com/elenio/flowcase/internal/platform/auth"
	"github.com/elenio/flowcase/internal/platform/events"
	"github.com/elenio/flowcase/internal/platform/idempotency"
	"github.com/elenio/flowcase/internal/platform/postgres"
	"github.com/elenio/flowcase/internal/workflows/catalog"
	"github.com/elenio/flowcase/internal/workflows/definition"
	"github.com/elenio/flowcase/internal/workflows/validator"
)

type Service struct {
	DB        *pgxpool.Pool
	Catalog   catalog.Catalog
	Templates []packs.Template
}

var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// --- authorization ------------------------------------------------------------------------------

func requireMember(a auth.Identity) error {
	if a.TenantID == "" || a.UserID == "" || len(a.Roles) == 0 {
		return ErrForbidden
	}
	return nil
}

func requireAny(a auth.Identity, roles ...auth.Role) error {
	for _, r := range roles {
		if a.HasRole(r) {
			return nil
		}
	}
	return ErrForbidden
}

// requirePublisher: publication is a human authorization act; service principals never qualify.
func requirePublisher(a auth.Identity) error {
	if !a.HasRole(auth.RoleWorkflowPublisher) || !a.IsHuman() {
		return ErrForbidden
	}
	return nil
}

func (s *Service) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	err := pgx.BeginFunc(ctx, s.DB, fn)
	if postgres.HasCode(err, postgres.CodeImmutable) {
		return ErrImmutable
	}
	return err
}

func userAudit(ctx context.Context, a auth.Identity, action, subjectType, subjectID, version string, data map[string]any) audit.Event {
	return audit.Event{TenantID: a.TenantID, ActorType: "user", ActorID: a.UserID, Action: action, SubjectType: subjectType,
		SubjectID: subjectID, SubjectVersion: version, TraceID: audit.TraceID(ctx), Data: data}
}

// --- document preparation -----------------------------------------------------------------------

type override struct {
	name, description string
	strictIdentity    bool // PATCH: body key/version must match the URL
}

// prepare enforces structural validity, pins key and version to server values, strips the
// server-managed status, and returns canonical content plus the semantic report.
func (s *Service) prepare(raw []byte, key string, version int, o override) ([]byte, definition.Report, error) {
	res := validator.Validate(raw, s.Catalog)
	if res.Definition == nil {
		return nil, res.Report, &ValidationError{Report: res.Report}
	}
	d := res.Definition
	if o.strictIdentity {
		var diags []definition.Diagnostic
		if d.Key != key {
			diags = append(diags, definition.Errorf("/key", "", "key_mismatch", "definition key %q does not match workflow %q", d.Key, key))
		}
		if d.Version != version {
			diags = append(diags, definition.Errorf("/version", "", "version_mismatch", "definition version %d does not match version %d", d.Version, version))
		}
		if len(diags) > 0 {
			return nil, definition.NewReport(diags), &ValidationError{Report: definition.NewReport(diags)}
		}
	}
	d.Key, d.Version, d.Status = key, version, ""
	if o.name != "" {
		d.Name = o.name
	}
	if o.description != "" {
		d.Description = o.description
	}
	content, err := definition.Canonical(*d)
	if err != nil {
		return nil, definition.Report{}, err
	}
	return content, validator.ValidateDefinition(d, s.Catalog).Report, nil
}

// decodeStored parses stored content into a typed definition.
func decodeStored(raw []byte) (*definition.Definition, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var d definition.Definition
	if err := dec.Decode(&d); err != nil {
		return nil, fmt.Errorf("stored definition unreadable: %w", err)
	}
	return &d, nil
}

// present renders stored content with the row's lifecycle status in schema field order.
func present(raw []byte, status string) (json.RawMessage, error) {
	d, err := decodeStored(raw)
	if err != nil {
		return nil, err
	}
	d.Status = status
	return json.Marshal(d)
}

func (s *Service) report(v Version) definition.Report {
	return validator.Validate(v.Definition, s.Catalog).Report
}

// --- reads --------------------------------------------------------------------------------------

const versionSelect = `
SELECT v.id::text, d.id::text, d.key, v.version, v.status, v.schema_version, v.definition, v.definition_hash,
       dv.version, v.change_note, v.created_by::text, cu.display_name, v.created_at, v.updated_at, v.validated_at,
       v.published_by::text, pu.display_name, v.published_at, v.deprecated_at, v.version_num
FROM workflow_versions v
JOIN workflow_definitions d ON d.id = v.definition_id AND d.tenant_id = v.tenant_id
JOIN users cu ON cu.id = v.created_by
LEFT JOIN users pu ON pu.id = v.published_by
LEFT JOIN workflow_versions dv ON dv.id = v.derived_from_version_id`

func scanVersion(row pgx.Row) (Version, string, error) {
	var v Version
	var defID string
	var content []byte
	err := row.Scan(&v.ID, &defID, &v.WorkflowKey, &v.Version, &v.Status, &v.SchemaVersion, &content, &v.DefinitionHash,
		&v.DerivedFromVersion, &v.ChangeNote, &v.CreatedBy, &v.CreatedByName, &v.CreatedAt, &v.UpdatedAt, &v.ValidatedAt,
		&v.PublishedBy, &v.PublishedByName, &v.PublishedAt, &v.DeprecatedAt, &v.VersionNum)
	if errors.Is(err, pgx.ErrNoRows) {
		return Version{}, "", ErrNotFound
	}
	if err != nil {
		return Version{}, "", err
	}
	v.Definition, err = present(content, v.Status)
	return v, defID, err
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Service) loadVersion(ctx context.Context, q querier, tenant, key string, version int, lock bool) (Version, string, error) {
	sql := versionSelect + ` WHERE v.tenant_id = $1 AND d.key = $2 AND v.version = $3`
	if lock {
		sql += ` FOR UPDATE OF v`
	}
	return scanVersion(q.QueryRow(ctx, sql, tenant, key, version))
}

func (s *Service) versionByID(ctx context.Context, q querier, tenant, id string) (Version, error) {
	v, _, err := scanVersion(q.QueryRow(ctx, versionSelect+` WHERE v.tenant_id = $1 AND v.id = $2`, tenant, id))
	return v, err
}

func (s *Service) GetVersion(ctx context.Context, a auth.Identity, key string, version int) (Version, error) {
	if err := requireMember(a); err != nil {
		return Version{}, err
	}
	v, _, err := s.loadVersion(ctx, s.DB, a.TenantID, key, version, false)
	return v, err
}

const summarySelect = `
SELECT id, key, name, description, domain_pack, latest_published, open_draft, version_count, node_count, updated_at FROM (
  SELECT d.id::text AS id, d.key, d.name, d.description, d.domain_pack, d.updated_at,
    (SELECT max(v.version) FROM workflow_versions v WHERE v.definition_id = d.id AND v.status = 'published') AS latest_published,
    (SELECT v.version FROM workflow_versions v WHERE v.definition_id = d.id AND v.status IN ('draft', 'validated')) AS open_draft,
    (SELECT count(*) FROM workflow_versions v WHERE v.definition_id = d.id)::int AS version_count,
    coalesce((SELECT jsonb_array_length(v.definition->'nodes') FROM workflow_versions v
              WHERE v.definition_id = d.id ORDER BY v.version DESC LIMIT 1), 0) AS node_count
  FROM workflow_definitions d
  WHERE d.tenant_id = $1 AND d.status = 'active'
) x`

func scanSummary(r pgx.CollectableRow) (DefinitionSummary, error) {
	var d DefinitionSummary
	err := r.Scan(&d.ID, &d.Key, &d.Name, &d.Description, &d.DomainPack, &d.LatestPublished, &d.OpenDraft, &d.VersionCount, &d.NodeCount, &d.UpdatedAt)
	return d, err
}

// ListDefinitions returns one page ordered by key and the cursor for the next page ("" when done).
func (s *Service) ListDefinitions(ctx context.Context, a auth.Identity, f ListFilter) ([]DefinitionSummary, string, error) {
	if err := requireMember(a); err != nil {
		return nil, "", err
	}
	if f.Limit <= 0 || f.Limit > 100 {
		f.Limit = 50
	}
	switch f.Status {
	case "", "all":
		f.Status = "all"
	case "published", "draft":
	default:
		return nil, "", &InputError{Code: "invalid_filter", Message: "status must be all, published or draft", Path: "status"}
	}
	rows, err := s.DB.Query(ctx, summarySelect+`
		WHERE key > $2
		  AND ($3 = '' OR strpos(lower(name), lower($3)) > 0 OR strpos(key, lower($3)) > 0)
		  AND ($4 = 'all' OR ($4 = 'published' AND latest_published IS NOT NULL) OR ($4 = 'draft' AND open_draft IS NOT NULL))
		ORDER BY key LIMIT $5`, a.TenantID, f.After, f.Query, f.Status, f.Limit+1)
	if err != nil {
		return nil, "", err
	}
	out, err := pgx.CollectRows(rows, scanSummary)
	if err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		next = out[len(out)-1].Key
	}
	if out == nil {
		out = []DefinitionSummary{}
	}
	return out, next, nil
}

func (s *Service) GetDefinition(ctx context.Context, a auth.Identity, key string) (DefinitionDetail, error) {
	if err := requireMember(a); err != nil {
		return DefinitionDetail{}, err
	}
	rows, err := s.DB.Query(ctx, summarySelect+` WHERE key = $2`, a.TenantID, key)
	if err != nil {
		return DefinitionDetail{}, err
	}
	sums, err := pgx.CollectRows(rows, scanSummary)
	if err != nil {
		return DefinitionDetail{}, err
	}
	if len(sums) == 0 {
		return DefinitionDetail{}, ErrNotFound
	}
	detail := DefinitionDetail{DefinitionSummary: sums[0]}
	rows, err = s.DB.Query(ctx, `
		SELECT v.id::text, v.version, v.status, v.definition_hash, v.change_note, jsonb_array_length(v.definition->'nodes'),
		       cu.display_name, v.created_at, v.updated_at, pu.display_name, v.published_at, v.deprecated_at, v.version_num
		FROM workflow_versions v JOIN users cu ON cu.id = v.created_by LEFT JOIN users pu ON pu.id = v.published_by
		WHERE v.tenant_id = $1 AND v.definition_id = $2 ORDER BY v.version DESC`, a.TenantID, detail.ID)
	if err != nil {
		return DefinitionDetail{}, err
	}
	detail.Versions, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (VersionSummary, error) {
		var v VersionSummary
		err := r.Scan(&v.ID, &v.Version, &v.Status, &v.DefinitionHash, &v.ChangeNote, &v.NodeCount, &v.CreatedByName,
			&v.CreatedAt, &v.UpdatedAt, &v.PublishedByName, &v.PublishedAt, &v.DeprecatedAt, &v.VersionNum)
		return v, err
	})
	return detail, err
}

// --- idempotency --------------------------------------------------------------------------------

// RequestHash fingerprints a command body for idempotency comparison.
func RequestHash(parts ...[]byte) string { return idempotency.RequestHash(parts...) }

func claim(ctx context.Context, tx pgx.Tx, a auth.Identity, scope string, idem Idempotency) (string, bool, error) {
	ref, replay, err := idempotency.Claim(ctx, tx, a.TenantID, a.UserID, scope, idempotency.Key{Key: idem.Key, Hash: idem.Hash})
	switch {
	case errors.Is(err, idempotency.ErrKeyRequired):
		return "", false, &InputError{Code: "idempotency_key_required", Message: err.Error(), Path: "Idempotency-Key"}
	case errors.Is(err, idempotency.ErrReused):
		return "", false, ErrIdempotencyReuse
	}
	return ref, replay, err
}

func complete(ctx context.Context, tx pgx.Tx, a auth.Identity, scope, key, ref string) error {
	return idempotency.Complete(ctx, tx, a.TenantID, a.UserID, scope, key, ref)
}

func (s *Service) replay(ctx context.Context, tx pgx.Tx, tenant, ref string) (SaveResult, error) {
	v, err := s.versionByID(ctx, tx, tenant, ref)
	if err != nil {
		return SaveResult{}, err
	}
	return SaveResult{Version: v, Report: s.report(v)}, nil
}

// --- commands -----------------------------------------------------------------------------------

func skeleton(key, name string) []byte {
	b, _ := json.Marshal(map[string]any{
		"schema_version": definition.SchemaVersion, "key": key, "name": name, "version": 1,
		"nodes": []any{
			map[string]any{"id": "start", "type": "start", "name": "Start", "next": []string{"end"}},
			map[string]any{"id": "end", "type": "end", "name": "End", "next": []string{}},
		},
	})
	return b
}

func (s *Service) template(key string) (packs.Template, bool) {
	for _, t := range s.Templates {
		if t.Key == key {
			return t, true
		}
	}
	return packs.Template{}, false
}

func (s *Service) createDefinitionTx(ctx context.Context, tx pgx.Tx, a auth.Identity, key string, doc []byte, o override, domainPack *string, data map[string]any) (SaveResult, error) {
	content, report, err := s.prepare(doc, key, 1, o)
	if err != nil {
		return SaveResult{}, err
	}
	d, err := decodeStored(content)
	if err != nil {
		return SaveResult{}, err
	}
	var defID string
	err = tx.QueryRow(ctx, `INSERT INTO workflow_definitions (tenant_id, key, name, description, domain_pack, created_by)
		VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (tenant_id, key) DO NOTHING RETURNING id::text`,
		a.TenantID, key, d.Name, d.Description, domainPack, a.UserID).Scan(&defID)
	if errors.Is(err, pgx.ErrNoRows) {
		return SaveResult{}, &ConflictError{Code: "key_exists", Message: fmt.Sprintf("a workflow with key %q already exists", key)}
	}
	if err != nil {
		return SaveResult{}, err
	}
	var versionID string
	if err := tx.QueryRow(ctx, `INSERT INTO workflow_versions (tenant_id, definition_id, version, schema_version, status, definition, created_by, updated_by)
		VALUES ($1, $2, 1, $3, 'draft', $4, $5, $5) RETURNING id::text`, a.TenantID, defID, d.SchemaVersion, content, a.UserID).Scan(&versionID); err != nil {
		return SaveResult{}, err
	}
	if data == nil {
		data = map[string]any{}
	}
	data["version"], data["node_count"] = 1, len(d.Nodes)
	if err := audit.Record(ctx, tx, userAudit(ctx, a, "workflow.created", "workflow", key, "1", data)); err != nil {
		return SaveResult{}, err
	}
	v, err := s.versionByID(ctx, tx, a.TenantID, versionID)
	return SaveResult{Version: v, Report: report}, err
}

// CreateDefinition creates a workflow with draft v1 from a template, an imported document, or a
// minimal start→end skeleton.
func (s *Service) CreateDefinition(ctx context.Context, a auth.Identity, idem Idempotency, in CreateInput) (SaveResult, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner); err != nil {
		return SaveResult{}, err
	}
	if !keyPattern.MatchString(in.Key) {
		return SaveResult{}, &InputError{Code: "invalid_key", Message: "key must be lowercase letters, digits and hyphens, starting with a letter", Path: "key"}
	}
	var doc []byte
	var pack *string
	data := map[string]any{}
	switch {
	case in.Template != "" && len(in.Definition) > 0:
		return SaveResult{}, &InputError{Code: "ambiguous_source", Message: "give either template or definition, not both", Path: "template"}
	case in.Template != "":
		t, ok := s.template(in.Template)
		if !ok {
			return SaveResult{}, &InputError{Code: "unknown_template", Message: fmt.Sprintf("template %q does not exist", in.Template), Path: "template"}
		}
		doc, pack = t.Document, &t.Pack
		data["template"] = t.Key
	case len(in.Definition) > 0:
		doc = in.Definition
		data["source"] = "import"
	default:
		if len([]rune(in.Name)) < 3 {
			return SaveResult{}, &InputError{Code: "invalid_name", Message: "name must have at least 3 characters", Path: "name"}
		}
		doc = skeleton(in.Key, in.Name)
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		ref, replayed, err := claim(ctx, tx, a, "workflow.create", idem)
		if err != nil {
			return err
		}
		if replayed {
			out, err = s.replay(ctx, tx, a.TenantID, ref)
			return err
		}
		if out, err = s.createDefinitionTx(ctx, tx, a, in.Key, doc, override{name: in.Name, description: in.Description}, pack, data); err != nil {
			return err
		}
		return complete(ctx, tx, a, "workflow.create", idem.Key, out.Version.ID)
	})
	return out, err
}

// Duplicate copies a version of one workflow into a new workflow's draft v1.
func (s *Service) Duplicate(ctx context.Context, a auth.Identity, idem Idempotency, srcKey string, in DuplicateInput) (SaveResult, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner); err != nil {
		return SaveResult{}, err
	}
	if !keyPattern.MatchString(in.Key) {
		return SaveResult{}, &InputError{Code: "invalid_key", Message: "key must be lowercase letters, digits and hyphens, starting with a letter", Path: "key"}
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		ref, replayed, err := claim(ctx, tx, a, "workflow.duplicate", idem)
		if err != nil {
			return err
		}
		if replayed {
			out, err = s.replay(ctx, tx, a.TenantID, ref)
			return err
		}
		from := in.FromVersion
		if from == 0 {
			err = tx.QueryRow(ctx, `SELECT coalesce(
				(SELECT max(v.version) FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.definition_id
				 WHERE d.tenant_id = $1 AND d.key = $2 AND v.status = 'published'),
				(SELECT max(v.version) FROM workflow_versions v JOIN workflow_definitions d ON d.id = v.definition_id
				 WHERE d.tenant_id = $1 AND d.key = $2), 0)`, a.TenantID, srcKey).Scan(&from)
			if err != nil {
				return err
			}
		}
		src, _, err := s.loadVersion(ctx, tx, a.TenantID, srcKey, from, false)
		if err != nil {
			return err
		}
		var pack *string
		_ = tx.QueryRow(ctx, `SELECT domain_pack FROM workflow_definitions WHERE tenant_id = $1 AND key = $2`, a.TenantID, srcKey).Scan(&pack)
		data := map[string]any{"duplicated_from": fmt.Sprintf("%s@v%d", srcKey, src.Version)}
		if out, err = s.createDefinitionTx(ctx, tx, a, in.Key, src.Definition, override{name: in.Name}, pack, data); err != nil {
			return err
		}
		return complete(ctx, tx, a, "workflow.duplicate", idem.Key, out.Version.ID)
	})
	return out, err
}

// CreateDraftVersion forks the next version from a published (or deprecated) one. Only one open
// draft may exist per workflow; existing versions and the cases pinned to them are untouched.
func (s *Service) CreateDraftVersion(ctx context.Context, a auth.Identity, idem Idempotency, key string, from int) (SaveResult, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner); err != nil {
		return SaveResult{}, err
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		ref, replayed, err := claim(ctx, tx, a, "workflow.version.create", idem)
		if err != nil {
			return err
		}
		if replayed {
			out, err = s.replay(ctx, tx, a.TenantID, ref)
			return err
		}
		var defID string
		err = tx.QueryRow(ctx, `SELECT id::text FROM workflow_definitions WHERE tenant_id = $1 AND key = $2 FOR UPDATE`, a.TenantID, key).Scan(&defID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		var open *int
		var maxVersion, latestReleased int
		if err := tx.QueryRow(ctx, `SELECT
			(SELECT version FROM workflow_versions WHERE definition_id = $1 AND status IN ('draft', 'validated')),
			(SELECT max(version) FROM workflow_versions WHERE definition_id = $1),
			coalesce((SELECT max(version) FROM workflow_versions WHERE definition_id = $1 AND status = 'published'),
			         (SELECT max(version) FROM workflow_versions WHERE definition_id = $1 AND status = 'deprecated'), 0)`,
			defID).Scan(&open, &maxVersion, &latestReleased); err != nil {
			return err
		}
		if open != nil {
			return &ConflictError{Code: "draft_exists", Message: fmt.Sprintf("version %d is already an open draft; edit or publish it first", *open)}
		}
		if from == 0 {
			from = latestReleased
		}
		src, _, err := s.loadVersion(ctx, tx, a.TenantID, key, from, false)
		if err != nil {
			return err
		}
		if src.Status != StatusPublished && src.Status != StatusDeprecated {
			return &ConflictError{Code: "invalid_state", Message: "new versions fork from a published or deprecated version"}
		}
		next := maxVersion + 1
		content, report, err := s.prepare(src.Definition, key, next, override{})
		if err != nil {
			return err
		}
		var id string
		if err := tx.QueryRow(ctx, `INSERT INTO workflow_versions (tenant_id, definition_id, version, schema_version, status, definition,
				derived_from_version_id, created_by, updated_by)
			VALUES ($1, $2, $3, $4, 'draft', $5, $6, $7, $7) RETURNING id::text`,
			a.TenantID, defID, next, src.SchemaVersion, content, src.ID, a.UserID).Scan(&id); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, "workflow.version.created", "workflow", key, strconv.Itoa(next),
			map[string]any{"derived_from_version": src.Version})); err != nil {
			return err
		}
		v, err := s.versionByID(ctx, tx, a.TenantID, id)
		if err != nil {
			return err
		}
		out = SaveResult{Version: v, Report: report}
		return complete(ctx, tx, a, "workflow.version.create", idem.Key, id)
	})
	return out, err
}

// UpdateDraft replaces a draft's content. ifMatch must equal the stored version_num.
func (s *Service) UpdateDraft(ctx context.Context, a auth.Identity, key string, version, ifMatch int, in UpdateInput) (SaveResult, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner); err != nil {
		return SaveResult{}, err
	}
	if len(in.Definition) == 0 {
		return SaveResult{}, &InputError{Code: "required", Message: "definition is required", Path: "definition"}
	}
	if in.ChangeNote != nil && len([]rune(*in.ChangeNote)) > 2000 {
		return SaveResult{}, &InputError{Code: "invalid_change_note", Message: "change_note is limited to 2000 characters", Path: "change_note"}
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		cur, _, err := s.loadVersion(ctx, tx, a.TenantID, key, version, true)
		if err != nil {
			return err
		}
		if cur.Status == StatusPublished || cur.Status == StatusDeprecated {
			return ErrImmutable
		}
		if cur.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		content, report, err := s.prepare(in.Definition, key, version, override{strictIdentity: true})
		if err != nil {
			return err
		}
		note := cur.ChangeNote
		if in.ChangeNote != nil {
			note = *in.ChangeNote
		}
		if _, err := tx.Exec(ctx, `UPDATE workflow_versions SET definition = $2, change_note = $3, status = 'draft', validated_at = NULL,
				updated_by = $4, updated_at = now(), version_num = version_num + 1 WHERE id = $1`,
			cur.ID, content, note, a.UserID); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, "workflow.version.updated", "workflow", key, strconv.Itoa(version),
			map[string]any{"valid": report.Valid, "error_count": len(report.Errors())})); err != nil {
			return err
		}
		v, err := s.versionByID(ctx, tx, a.TenantID, cur.ID)
		out = SaveResult{Version: v, Report: report}
		return err
	})
	return out, err
}

// ValidateVersion re-validates stored content server-side. A clean draft becomes "validated";
// a previously validated draft that no longer passes (e.g. a policy was unregistered) drops back.
func (s *Service) ValidateVersion(ctx context.Context, a auth.Identity, key string, version int) (SaveResult, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher); err != nil {
		return SaveResult{}, err
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		cur, _, err := s.loadVersion(ctx, tx, a.TenantID, key, version, true)
		if err != nil {
			return err
		}
		report := s.report(cur)
		out = SaveResult{Version: cur, Report: report}
		if cur.Status == StatusPublished || cur.Status == StatusDeprecated {
			return nil
		}
		next := StatusDraft
		if report.Valid {
			next = StatusValidated
		}
		if next != cur.Status {
			if _, err := tx.Exec(ctx, `UPDATE workflow_versions SET status = $2, validated_at = CASE WHEN $2 = 'validated' THEN now() END,
					updated_at = now(), version_num = version_num + 1 WHERE id = $1`, cur.ID, next); err != nil {
				return err
			}
		}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, "workflow.version.validated", "workflow", key, strconv.Itoa(version),
			map[string]any{"valid": report.Valid, "error_count": len(report.Errors())})); err != nil {
			return err
		}
		out.Version, err = s.versionByID(ctx, tx, a.TenantID, cur.ID)
		return err
	})
	return out, err
}

// ValidateDocument validates unsaved content without storing anything.
func (s *Service) ValidateDocument(_ context.Context, a auth.Identity, raw []byte) (definition.Report, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher); err != nil {
		return definition.Report{}, err
	}
	return validator.Validate(raw, s.Catalog).Report, nil
}

// Publish re-validates on the server, freezes the content hash and makes the version available for
// new cases. The database trigger independently forbids any later content change.
func (s *Service) Publish(ctx context.Context, a auth.Identity, key string, version, ifMatch int) (SaveResult, error) {
	if err := requirePublisher(a); err != nil {
		return SaveResult{}, err
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		cur, defID, err := s.loadVersion(ctx, tx, a.TenantID, key, version, true)
		if err != nil {
			return err
		}
		if cur.Status == StatusPublished || cur.Status == StatusDeprecated {
			return ErrImmutable
		}
		if cur.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		res := validator.Validate(cur.Definition, s.Catalog)
		if !res.Report.Valid {
			return &ValidationError{Report: res.Report}
		}
		hash, err := definition.Hash(*res.Definition)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workflow_versions SET status = 'published', definition_hash = $2, published_by = $3,
				published_at = now(), validated_at = coalesce(validated_at, now()), updated_by = $3, updated_at = now(),
				version_num = version_num + 1 WHERE id = $1`, cur.ID, hash, a.UserID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE workflow_definitions SET name = $2, description = $3, updated_at = now(),
				version_num = version_num + 1 WHERE id = $1`, defID, res.Definition.Name, res.Definition.Description); err != nil {
			return err
		}
		data := map[string]any{"definition_hash": hash, "policy_refs": res.Definition.PolicyRefs, "node_count": len(res.Definition.Nodes)}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, "workflow.version.published", "workflow", key, strconv.Itoa(version), data)); err != nil {
			return err
		}
		if err := events.Enqueue(ctx, tx, events.Outbox{TenantID: a.TenantID, Topic: "workflow.version.published", AggregateType: "workflow_version",
			AggregateID: cur.ID, DedupeKey: "workflow.version.published:" + cur.ID, TraceID: audit.TraceID(ctx),
			Payload: map[string]any{"workflow_key": key, "version": version, "definition_hash": hash}}); err != nil {
			return err
		}
		out.Version, err = s.versionByID(ctx, tx, a.TenantID, cur.ID)
		out.Report = res.Report
		return err
	})
	return out, err
}

// Deprecate stops a published version from being chosen for new cases; running cases stay pinned.
func (s *Service) Deprecate(ctx context.Context, a auth.Identity, key string, version, ifMatch int) (SaveResult, error) {
	if err := requirePublisher(a); err != nil {
		return SaveResult{}, err
	}
	var out SaveResult
	err := s.tx(ctx, func(tx pgx.Tx) error {
		cur, _, err := s.loadVersion(ctx, tx, a.TenantID, key, version, true)
		if err != nil {
			return err
		}
		if cur.Status != StatusPublished {
			return &ConflictError{Code: "invalid_state", Message: "only published versions can be deprecated"}
		}
		if cur.VersionNum != ifMatch {
			return ErrPreconditionFailed
		}
		if _, err := tx.Exec(ctx, `UPDATE workflow_versions SET status = 'deprecated', deprecated_by = $2, deprecated_at = now(),
				updated_at = now(), version_num = version_num + 1 WHERE id = $1`, cur.ID, a.UserID); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, userAudit(ctx, a, "workflow.version.deprecated", "workflow", key, strconv.Itoa(version),
			map[string]any{"definition_hash": cur.DefinitionHash})); err != nil {
			return err
		}
		if err := events.Enqueue(ctx, tx, events.Outbox{TenantID: a.TenantID, Topic: "workflow.version.deprecated", AggregateType: "workflow_version",
			AggregateID: cur.ID, DedupeKey: "workflow.version.deprecated:" + cur.ID, TraceID: audit.TraceID(ctx),
			Payload: map[string]any{"workflow_key": key, "version": version}}); err != nil {
			return err
		}
		out.Version, err = s.versionByID(ctx, tx, a.TenantID, cur.ID)
		out.Report = s.report(out.Version)
		return err
	})
	return out, err
}

// ListAudit is limited to roles that govern workflows in Phase 1; case-scoped audit arrives with cases.
func (s *Service) ListAudit(ctx context.Context, a auth.Identity, f audit.Filter) ([]audit.Entry, int64, error) {
	if err := requireAny(a, auth.RoleWorkflowDesigner, auth.RoleWorkflowPublisher, auth.RoleTenantAdmin); err != nil {
		return nil, 0, err
	}
	return audit.List(ctx, s.DB, a.TenantID, f)
}
