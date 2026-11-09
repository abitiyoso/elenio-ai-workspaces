// Package registry manages workflow definitions and their immutable published versions (WCP-01):
// draft editing with optimistic concurrency, server-side validation, authorized publication,
// deprecation, duplication and version forking. Every mutation writes audit in the same transaction.
package registry

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/elenio/flowcase/internal/workflows/definition"
)

const (
	StatusDraft      = "draft"
	StatusValidated  = "validated"
	StatusPublished  = "published"
	StatusDeprecated = "deprecated"
)

var (
	ErrForbidden          = errors.New("forbidden")
	ErrNotFound           = errors.New("not found")
	ErrImmutable          = errors.New("published workflow versions are immutable")
	ErrPreconditionFailed = errors.New("version precondition failed")
	ErrIdempotencyReuse   = errors.New("idempotency key was used for a different request")
)

// ConflictError is a 409 with a stable code (key_exists, draft_exists, invalid_state).
type ConflictError struct{ Code, Message string }

func (e *ConflictError) Error() string { return e.Message }

// ValidationError carries the validator report; the API maps it to 422 with per-path details.
type ValidationError struct{ Report definition.Report }

func (e *ValidationError) Error() string { return "workflow definition is invalid" }

// InputError is a 400 for malformed command input (not definition content).
type InputError struct{ Code, Message, Path string }

func (e *InputError) Error() string { return e.Message }

type Version struct {
	ID                 string          `json:"id"`
	WorkflowKey        string          `json:"workflow_key"`
	Version            int             `json:"version"`
	Status             string          `json:"status"`
	SchemaVersion      string          `json:"schema_version"`
	Definition         json.RawMessage `json:"definition"`
	DefinitionHash     *string         `json:"definition_hash"`
	DerivedFromVersion *int            `json:"derived_from_version"`
	ChangeNote         string          `json:"change_note"`
	CreatedBy          string          `json:"created_by"`
	CreatedByName      string          `json:"created_by_name"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	ValidatedAt        *time.Time      `json:"validated_at"`
	PublishedBy        *string         `json:"published_by"`
	PublishedByName    *string         `json:"published_by_name"`
	PublishedAt        *time.Time      `json:"published_at"`
	DeprecatedAt       *time.Time      `json:"deprecated_at"`
	VersionNum         int             `json:"version_num"`
}

type VersionSummary struct {
	ID              string     `json:"id"`
	Version         int        `json:"version"`
	Status          string     `json:"status"`
	DefinitionHash  *string    `json:"definition_hash"`
	ChangeNote      string     `json:"change_note"`
	NodeCount       int        `json:"node_count"`
	CreatedByName   string     `json:"created_by_name"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	PublishedByName *string    `json:"published_by_name"`
	PublishedAt     *time.Time `json:"published_at"`
	DeprecatedAt    *time.Time `json:"deprecated_at"`
	VersionNum      int        `json:"version_num"`
}

type DefinitionSummary struct {
	ID              string    `json:"id"`
	Key             string    `json:"key"`
	Name            string    `json:"name"`
	Description     string    `json:"description"`
	DomainPack      *string   `json:"domain_pack"`
	LatestPublished *int      `json:"latest_published_version"`
	OpenDraft       *int      `json:"open_draft_version"`
	VersionCount    int       `json:"version_count"`
	NodeCount       int       `json:"node_count"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type DefinitionDetail struct {
	DefinitionSummary
	Versions []VersionSummary `json:"versions"`
}

type ListFilter struct {
	Status string // all | published | draft
	Query  string
	After  string // cursor: last key of the previous page
	Limit  int
}

// Idempotency identifies a create command; Hash is the SHA-256 of the request body.
type Idempotency struct {
	Key  string
	Hash string
}

type CreateInput struct {
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Template    string          `json:"template"`   // template workflow key from a domain pack
	Definition  json.RawMessage `json:"definition"` // or a full document (import)
}

type DuplicateInput struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	FromVersion int    `json:"from_version"` // 0 = latest published, else latest version
}

type UpdateInput struct {
	Definition json.RawMessage `json:"definition"`
	ChangeNote *string         `json:"change_note"`
}

// SaveResult returns the stored version plus the current validation report, so editors can show
// problems without a second call. Drafts may be saved with semantic errors; publication may not.
type SaveResult struct {
	Version Version           `json:"version"`
	Report  definition.Report `json:"validation"`
}
