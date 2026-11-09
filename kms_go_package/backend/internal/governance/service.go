package governance

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
)

var (
	ErrForbidden = errors.New("governance access denied")
	ErrNotFound  = errors.New("governance resource not found")
)

type Service struct {
	db *sql.DB
	// selfApprovalExempt holds principals allowed to review their own submissions (development walkthroughs
	// only; config refuses it elsewhere). Everyone else stays under separation of duties.
	selfApprovalExempt map[string]bool
}

func NewService(db *sql.DB) *Service { return &Service{db: db} }

// WithSelfApprovalExempt returns the service with the given principals exempt from the self-review rule.
func (s *Service) WithSelfApprovalExempt(principalIDs []string) *Service {
	exempt := make(map[string]bool, len(principalIDs))
	for _, id := range principalIDs {
		exempt[id] = true
	}
	s.selfApprovalExempt = exempt
	return s
}

type Version struct {
	ID                string     `json:"id"`
	VersionNumber     int        `json:"version_number"`
	ChecksumSHA256    string     `json:"checksum_sha256"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	ParserStatus      string     `json:"parser_status"`
	IndexStatus       string     `json:"index_status"`
	ReviewStatus      string     `json:"review_status"`
	TrustStatus       string     `json:"trust_status"`
	PublicationStatus string     `json:"publication_status"`
	IsCurrent         bool       `json:"is_current"`
	Capabilities      []string   `json:"capabilities"`
	ValidFrom         *time.Time `json:"valid_from,omitempty"`
	ExpiresAt         *time.Time `json:"expires_at,omitempty"`
}

type Review struct {
	ID             string    `json:"id"`
	DocumentID     string    `json:"document_id"`
	VersionID      string    `json:"version_id"`
	DocumentTitle  string    `json:"document_title"`
	VersionNumber  int       `json:"version_number"`
	SubmittedBy    string    `json:"submitted_by"`
	SubmissionNote string    `json:"submission_note"`
	SubmittedAt    time.Time `json:"submitted_at"`
	Status         string    `json:"status"`
	Capabilities   []string  `json:"capabilities"`
}

type Activity struct {
	ID           string          `json:"id"`
	ActorType    string          `json:"actor_type"`
	ActorID      string          `json:"actor_id"`
	Action       string          `json:"action"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	VersionID    string          `json:"version_id,omitempty"`
	Outcome      string          `json:"outcome"`
	Rationale    string          `json:"rationale,omitempty"`
	Details      json.RawMessage `json:"details"`
	CreatedAt    time.Time       `json:"created_at"`
}

type target struct {
	DocumentID       string
	VersionID        string
	CollectionID     string
	CaseID           string
	DocumentAuthor   string
	VersionAuthor    string
	PublishedVersion string
	CurrentVersion   string
	DeletedAt        sql.NullTime
	RevokedAt        sql.NullTime
	VersionRevokedAt sql.NullTime
	ExpiresAt        sql.NullTime
	ParserStatus     string
	IndexStatus      string
	ReviewStatus     string
	TrustStatus      string
	Publication      string
	VersionNumber    int
	Checksum         string
	CreatedAt        time.Time
}

func (s *Service) SubmitReview(ctx context.Context, identity auth.Identity, documentID, versionID, note, correlationID string) (Review, error) {
	note = strings.TrimSpace(note)
	if len(note) > 2000 {
		return Review{}, fmt.Errorf("submission note exceeds 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Review{}, err
	}
	defer tx.Rollback()
	t, err := loadTarget(ctx, tx, identity.TenantID, documentID, versionID, true)
	if err != nil {
		return Review{}, mapNotFound(err)
	}
	if err := ensureLive(t); err != nil {
		return Review{}, err
	}
	role, err := membershipRole(ctx, tx, identity, t)
	if err != nil {
		return Review{}, err
	}
	if !canContribute(role) || t.ParserStatus != "success" || t.IndexStatus != "indexed" || (t.ExpiresAt.Valid && !t.ExpiresAt.Time.After(time.Now())) {
		return Review{}, ErrForbidden
	}
	if t.ReviewStatus != "draft" && t.ReviewStatus != "rejected" {
		return Review{}, fmt.Errorf("only draft or rejected versions can be submitted")
	}
	var review Review
	err = tx.QueryRowContext(ctx, `INSERT INTO document_reviews(tenant_id,document_id,version_id,submitted_by,submission_note)
	VALUES($1,$2,$3,$4,$5) RETURNING id::text,document_id::text,version_id::text,submitted_by::text,submission_note,submitted_at,status`, identity.TenantID, documentID, versionID, identity.UserID, note).
		Scan(&review.ID, &review.DocumentID, &review.VersionID, &review.SubmittedBy, &review.SubmissionNote, &review.SubmittedAt, &review.Status)
	if err != nil {
		return Review{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET review_status='pending',submitted_by=$3,submitted_at=now(),publication_status=CASE WHEN trust_status='customer_source' THEN publication_status ELSE 'ready_for_review' END WHERE tenant_id=$1 AND id=$2`, identity.TenantID, versionID, identity.UserID); err != nil {
		return Review{}, err
	}
	if t.PublishedVersion == "" {
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET lifecycle_status='ready_for_review',updated_at=now() WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, identity.TenantID, documentID); err != nil {
			return Review{}, err
		}
	}
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "review.submit", "document_version", versionID, t.CollectionID, t.CaseID, versionID, "allowed", note, correlationID, map[string]any{"review_id": review.ID}); err != nil {
		return Review{}, err
	}
	if err := tx.Commit(); err != nil {
		return Review{}, err
	}
	return review, nil
}

func (s *Service) ListReviews(ctx context.Context, identity auth.Identity) ([]Review, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id::text,r.document_id::text,r.version_id::text,d.title,v.version_number,r.submitted_by::text,r.submission_note,r.submitted_at,r.status
	FROM document_reviews r JOIN documents d ON d.tenant_id=r.tenant_id AND d.id=r.document_id
	JOIN document_versions v ON v.tenant_id=r.tenant_id AND v.id=r.version_id
	WHERE r.tenant_id=$1 AND r.status='pending' AND ($3 OR (r.submitted_by<>$2 AND v.created_by<>$2)) AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	AND ((d.case_id IS NULL AND EXISTS(SELECT 1 FROM collection_memberships m WHERE m.tenant_id=d.tenant_id AND m.collection_id=d.collection_id AND m.principal_id=$2 AND m.membership_role IN ('owner','steward','reviewer')))
	OR (d.case_id IS NOT NULL AND EXISTS(SELECT 1 FROM case_memberships cm WHERE cm.tenant_id=d.tenant_id AND cm.case_id=d.case_id AND cm.principal_id=$2 AND cm.membership_role IN ('owner','steward','reviewer'))))
	ORDER BY r.submitted_at ASC LIMIT 200`, identity.TenantID, identity.UserID, s.selfApprovalExempt[identity.UserID])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Review{}
	for rows.Next() {
		var item Review
		if err := rows.Scan(&item.ID, &item.DocumentID, &item.VersionID, &item.DocumentTitle, &item.VersionNumber, &item.SubmittedBy, &item.SubmissionNote, &item.SubmittedAt, &item.Status); err != nil {
			return nil, err
		}
		item.Capabilities = []string{"review"}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) DecideReview(ctx context.Context, identity auth.Identity, reviewID, decision, rationale, correlationID string) error {
	decision = strings.TrimSpace(decision)
	rationale = strings.TrimSpace(rationale)
	if decision != "approved" && decision != "rejected" {
		return fmt.Errorf("decision must be approved or rejected")
	}
	if rationale == "" || len(rationale) > 2000 {
		return fmt.Errorf("decision rationale must contain 1 to 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var t target
	var submitter string
	var reviewStatus string
	err = tx.QueryRowContext(ctx, `SELECT d.id::text,v.id::text,d.collection_id::text,COALESCE(d.case_id::text,''),d.created_by::text,v.created_by::text,COALESCE(d.published_version_id::text,''),d.deleted_at,d.revoked_at,v.revoked_at,v.expires_at,v.parser_status,v.index_status,v.review_status,v.trust_status,v.publication_status,v.version_number,v.checksum_sha256,v.created_at,r.submitted_by::text,r.status
	FROM document_reviews r JOIN documents d ON d.tenant_id=r.tenant_id AND d.id=r.document_id
	JOIN document_versions v ON v.tenant_id=r.tenant_id AND v.id=r.version_id
	WHERE r.tenant_id=$1 AND r.id=$2 FOR UPDATE OF d,v,r`, identity.TenantID, reviewID).
		Scan(&t.DocumentID, &t.VersionID, &t.CollectionID, &t.CaseID, &t.DocumentAuthor, &t.VersionAuthor, &t.PublishedVersion, &t.DeletedAt, &t.RevokedAt, &t.VersionRevokedAt, &t.ExpiresAt, &t.ParserStatus, &t.IndexStatus, &t.ReviewStatus, &t.TrustStatus, &t.Publication, &t.VersionNumber, &t.Checksum, &t.CreatedAt, &submitter, &reviewStatus)
	if err != nil {
		return mapNotFound(err)
	}
	if reviewStatus != "pending" || t.ReviewStatus != "pending" || ensureLive(t) != nil {
		return ErrForbidden
	}
	role, err := membershipRole(ctx, tx, identity, t)
	allowed, err := reviewMembershipAllowed(role, err)
	if err != nil {
		return err
	}
	if !allowed {
		return recordDeniedReview(ctx, tx, identity, t, decision, rationale, correlationID, nil)
	}
	selfReview := selfApprovalDenied(identity.UserID, submitter, t.VersionAuthor)
	exempt := selfReview && s.selfApprovalExempt[identity.UserID]
	if selfReview && !exempt {
		if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "review."+decision, "document_version", t.VersionID, t.CollectionID, t.CaseID, t.VersionID, "denied", rationale, correlationID, map[string]any{"reason": "self_review"}); err != nil {
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		return ErrForbidden
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO review_decisions(tenant_id,review_id,document_id,version_id,submitter_id,reviewer_id,decision,rationale,self_approval_exempt) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, identity.TenantID, reviewID, t.DocumentID, t.VersionID, submitter, identity.UserID, decision, rationale, exempt); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_reviews SET status=$3,resolved_at=now() WHERE tenant_id=$1 AND id=$2 AND status='pending'`, identity.TenantID, reviewID, decision); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET review_status=$3,reviewed_by=$4,reviewed_at=now(),trust_status=CASE WHEN $3='approved' AND trust_status='unverified' THEN 'approved' ELSE trust_status END,publication_status=CASE WHEN trust_status='customer_source' THEN publication_status WHEN $3='rejected' THEN 'draft' ELSE publication_status END,rejection_rationale=CASE WHEN $3='rejected' THEN $5 ELSE NULL END WHERE tenant_id=$1 AND id=$2`, identity.TenantID, t.VersionID, decision, identity.UserID, rationale); err != nil {
		return err
	}
	if t.PublishedVersion == "" {
		lifecycle := "ready_for_review"
		if decision == "rejected" {
			lifecycle = "draft"
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET lifecycle_status=$3,updated_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, t.DocumentID, lifecycle); err != nil {
			return err
		}
	}
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "review."+decision, "document_version", t.VersionID, t.CollectionID, t.CaseID, t.VersionID, "allowed", rationale, correlationID, reviewAuditDetails(reviewID, exempt)); err != nil {
		return err
	}
	return tx.Commit()
}

func recordDeniedReview(ctx context.Context, tx *sql.Tx, identity auth.Identity, t target, decision, rationale, correlationID string, details any) error {
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "review."+decision, "document_version", t.VersionID, t.CollectionID, t.CaseID, t.VersionID, "denied", rationale, correlationID, details); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return ErrForbidden
}

func reviewMembershipAllowed(role string, err error) (bool, error) {
	if err != nil && !errors.Is(err, ErrForbidden) {
		return false, err
	}
	if err != nil {
		return false, nil
	}
	return canReview(role), nil
}

func (s *Service) PublishVersion(ctx context.Context, identity auth.Identity, documentID, versionID, rationale, correlationID string) error {
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > 2000 {
		return fmt.Errorf("publication rationale must contain 1 to 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := loadTarget(ctx, tx, identity.TenantID, documentID, versionID, true)
	if err != nil {
		return mapNotFound(err)
	}
	if err := ensureLive(t); err != nil {
		return err
	}
	role, err := membershipRole(ctx, tx, identity, t)
	if err != nil || !canSteward(role) {
		return ErrForbidden
	}
	if t.CaseID != "" || t.TrustStatus == "customer_source" {
		return fmt.Errorf("case-private customer sources cannot be published as enterprise knowledge")
	}
	if t.VersionID != t.CurrentVersion || t.ReviewStatus != "approved" || t.TrustStatus != "approved" || t.ParserStatus != "success" || t.IndexStatus != "indexed" || (t.ExpiresAt.Valid && !t.ExpiresAt.Time.After(time.Now())) {
		return fmt.Errorf("only approved, indexed, current and unexpired versions can be published")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET publication_status='published',published_at=COALESCE(published_at,now()),valid_from=COALESCE(valid_from,now()) WHERE tenant_id=$1 AND id=$2`, identity.TenantID, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET published_version_id=$3,lifecycle_status='published',revoked_at=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, documentID, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO publication_events(tenant_id,document_id,version_id,actor_id,event_type,rationale) VALUES($1,$2,$3,$4,'published',$5)`, identity.TenantID, documentID, versionID, identity.UserID, rationale); err != nil {
		return err
	}
	if err := enqueueOutbox(ctx, tx, identity.TenantID, "version.publish", documentID, map[string]any{"version_id": versionID, "trust_status": "approved", "publication_status": "published"}); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "document.publish", "document", documentID, t.CollectionID, t.CaseID, versionID, "allowed", rationale, correlationID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) RevokeVersion(ctx context.Context, identity auth.Identity, documentID, versionID, rationale, correlationID string) error {
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > 2000 {
		return fmt.Errorf("revocation rationale must contain 1 to 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := loadTarget(ctx, tx, identity.TenantID, documentID, versionID, true)
	if err != nil {
		return mapNotFound(err)
	}
	if t.DeletedAt.Valid {
		return ErrNotFound
	}
	role, err := membershipRole(ctx, tx, identity, t)
	if err != nil || !canSteward(role) {
		return ErrForbidden
	}
	if t.Publication == "revoked" {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET publication_status='revoked',revoked_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET published_version_id=CASE WHEN published_version_id=$3 THEN NULL ELSE published_version_id END,
	lifecycle_status=CASE WHEN published_version_id=$3 THEN 'revoked' ELSE lifecycle_status END,
	updated_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, documentID, versionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO publication_events(tenant_id,document_id,version_id,actor_id,event_type,rationale) VALUES($1,$2,$3,$4,'revoked',$5)`, identity.TenantID, documentID, versionID, identity.UserID, rationale); err != nil {
		return err
	}
	if err := enqueueOutbox(ctx, tx, identity.TenantID, "version.delete_index", documentID, map[string]any{"version_id": versionID}); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "document.revoke", "document", documentID, t.CollectionID, t.CaseID, versionID, "allowed", rationale, correlationID, nil); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) SetExpiry(ctx context.Context, identity auth.Identity, documentID, versionID string, expiresAt *time.Time, rationale, correlationID string) error {
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > 2000 {
		return fmt.Errorf("expiry rationale must contain 1 to 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := loadTarget(ctx, tx, identity.TenantID, documentID, versionID, true)
	if err != nil {
		return mapNotFound(err)
	}
	if err := ensureDocumentLive(t); err != nil {
		return err
	}
	role, err := membershipRole(ctx, tx, identity, t)
	if err != nil || !canSteward(role) {
		return ErrForbidden
	}
	if expiresAt != nil && !expiresAt.After(time.Now()) {
		if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET expires_at=$3,publication_status='expired' WHERE tenant_id=$1 AND id=$2`, identity.TenantID, versionID, *expiresAt); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE documents SET published_version_id=CASE WHEN published_version_id=$3 THEN NULL ELSE published_version_id END,
		lifecycle_status=CASE WHEN published_version_id=$3 THEN 'expired' ELSE lifecycle_status END,updated_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, documentID, versionID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO publication_events(tenant_id,document_id,version_id,actor_id,event_type,rationale) VALUES($1,$2,$3,$4,'expired',$5)`, identity.TenantID, documentID, versionID, identity.UserID, rationale); err != nil {
			return err
		}
		if err := enqueueOutbox(ctx, tx, identity.TenantID, "version.delete_index", documentID, map[string]any{"version_id": versionID}); err != nil {
			return err
		}
	} else if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET expires_at=$3::timestamptz,publication_status=CASE WHEN $3::timestamptz IS NULL AND publication_status='expired' THEN 'draft' ELSE publication_status END WHERE tenant_id=$1 AND id=$2`, identity.TenantID, versionID, expiresAt); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "document.expiry.update", "document", documentID, t.CollectionID, t.CaseID, versionID, "allowed", rationale, correlationID, map[string]any{"expires_at": expiresAt}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) DeleteDocument(ctx context.Context, identity auth.Identity, documentID, rationale, correlationID string) error {
	rationale = strings.TrimSpace(rationale)
	if rationale == "" || len(rationale) > 2000 {
		return fmt.Errorf("deletion rationale must contain 1 to 2000 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	t, err := loadTarget(ctx, tx, identity.TenantID, documentID, "", true)
	if err != nil {
		return mapNotFound(err)
	}
	role, err := membershipRole(ctx, tx, identity, t)
	if err != nil || !canSteward(role) {
		return ErrForbidden
	}
	if t.DeletedAt.Valid {
		return tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT v.id::text,v.original_object_key FROM document_versions v WHERE v.tenant_id=$1 AND v.document_id=$2`, identity.TenantID, documentID)
	if err != nil {
		return err
	}
	versionIDs := []string{}
	objectKeys := []string{}
	for rows.Next() {
		var id, key string
		if err := rows.Scan(&id, &key); err != nil {
			rows.Close()
			return err
		}
		versionIDs = append(versionIDs, id)
		objectKeys = append(objectKeys, key)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	artifactRows, err := tx.QueryContext(ctx, `SELECT a.object_key FROM parser_artifacts a JOIN document_versions v ON v.tenant_id=a.tenant_id AND v.id=a.version_id WHERE a.tenant_id=$1 AND v.document_id=$2`, identity.TenantID, documentID)
	if err != nil {
		return err
	}
	for artifactRows.Next() {
		var key string
		if err := artifactRows.Scan(&key); err != nil {
			artifactRows.Close()
			return err
		}
		objectKeys = append(objectKeys, key)
	}
	if err := artifactRows.Close(); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET deleted_at=now(),revoked_at=now(),published_version_id=NULL,lifecycle_status='revoked',updated_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE document_versions SET publication_status='revoked',revoked_at=now() WHERE tenant_id=$1 AND document_id=$2`, identity.TenantID, documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE ingestion_jobs SET status='failed',stage='cancelled',last_error='document deleted',lease_expires_at=NULL,updated_at=now() WHERE tenant_id=$1 AND document_id=$2 AND status IN ('queued','processing')`, identity.TenantID, documentID); err != nil {
		return err
	}
	if err := enqueueOutbox(ctx, tx, identity.TenantID, "document.delete", documentID, map[string]any{"version_ids": versionIDs, "object_keys": objectKeys}); err != nil {
		return err
	}
	if err := appendAudit(ctx, tx, identity.TenantID, "user", identity.UserID, "document.delete", "document", documentID, t.CollectionID, t.CaseID, "", "allowed", rationale, correlationID, map[string]any{"version_count": len(versionIDs)}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM document_chunks WHERE tenant_id=$1 AND document_id=$2`, identity.TenantID, documentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM parser_artifacts WHERE tenant_id=$1 AND version_id=ANY($2::uuid[])`, identity.TenantID, versionIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) Versions(ctx context.Context, identity auth.Identity, documentID string) ([]Version, error) {
	t, err := loadTarget(ctx, s.db, identity.TenantID, documentID, "", false)
	if err != nil {
		return nil, mapNotFound(err)
	}
	if err := ensureDocumentLive(t); err != nil {
		return nil, err
	}
	role, err := membershipRole(ctx, s.db, identity, t)
	if err != nil || role == "" {
		return nil, ErrNotFound
	}
	rows, err := s.db.QueryContext(ctx, `SELECT v.id::text,v.version_number,v.checksum_sha256,v.created_by::text,COALESCE(v.submitted_by::text,''),v.created_at,v.parser_status,v.index_status,v.review_status,v.trust_status,v.publication_status,v.valid_from,v.expires_at,v.revoked_at,(d.current_version_id=v.id)
	FROM document_versions v JOIN documents d ON d.tenant_id=v.tenant_id AND d.id=v.document_id WHERE v.tenant_id=$1 AND v.document_id=$2 ORDER BY v.version_number DESC`, identity.TenantID, documentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Version{}
	for rows.Next() {
		var item Version
		var validFrom, expiresAt, revokedAt sql.NullTime
		var submittedBy string
		if err := rows.Scan(&item.ID, &item.VersionNumber, &item.ChecksumSHA256, &item.CreatedBy, &submittedBy, &item.CreatedAt, &item.ParserStatus, &item.IndexStatus, &item.ReviewStatus, &item.TrustStatus, &item.PublicationStatus, &validFrom, &expiresAt, &revokedAt, &item.IsCurrent); err != nil {
			return nil, err
		}
		if validFrom.Valid {
			item.ValidFrom = &validFrom.Time
		}
		if expiresAt.Valid {
			item.ExpiresAt = &expiresAt.Time
		}
		isLive := !t.DeletedAt.Valid && !t.RevokedAt.Valid && !revokedAt.Valid && (!expiresAt.Valid || expiresAt.Time.After(time.Now()))
		item.Capabilities = versionCapabilities(role, t.CaseID != "", item.IsCurrent, identity.UserID, submittedBy, item, s.selfApprovalExempt[identity.UserID])
		if !isLive {
			item.Capabilities = []string{}
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func versionCapabilities(role string, caseScoped, isCurrent bool, actorID, submittedBy string, version Version, selfApprovalExempt bool) []string {
	capabilities := []string{}
	if canContribute(role) {
		capabilities = append(capabilities, "upload_version")
		if (version.ReviewStatus == "draft" || version.ReviewStatus == "rejected") && version.ParserStatus == "success" && version.IndexStatus == "indexed" {
			capabilities = append(capabilities, "submit_review")
		}
	}
	if canReview(role) && version.ReviewStatus == "pending" && (selfApprovalExempt || (actorID != version.CreatedBy && actorID != submittedBy)) {
		capabilities = append(capabilities, "review")
	}
	if canSteward(role) {
		if isCurrent && !caseScoped && version.ReviewStatus == "approved" && version.TrustStatus == "approved" && version.ParserStatus == "success" && version.IndexStatus == "indexed" {
			capabilities = append(capabilities, "publish")
		}
		if version.PublicationStatus != "revoked" && version.PublicationStatus != "expired" {
			capabilities = append(capabilities, "revoke", "set_expiry", "delete_document")
		}
	}
	return capabilities
}

func (s *Service) Activity(ctx context.Context, identity auth.Identity, collectionID, caseID string, limit int) ([]Activity, error) {
	if limit < 1 || limit > 200 {
		limit = 100
	}
	var rows *sql.Rows
	var err error
	if caseID != "" {
		var role string
		err = s.db.QueryRowContext(ctx, `SELECT membership_role FROM case_memberships WHERE tenant_id=$1 AND case_id=$2 AND principal_id=$3`, identity.TenantID, caseID, identity.UserID).Scan(&role)
		if err != nil || !canReview(role) {
			return nil, ErrForbidden
		}
		rows, err = s.db.QueryContext(ctx, `SELECT id::text,actor_type,actor_id,action,resource_type,resource_id,COALESCE(version_id::text,''),outcome,rationale,details,created_at
		FROM audit_events WHERE tenant_id=$1 AND case_id=$2 ORDER BY created_at DESC LIMIT $3`, identity.TenantID, caseID, limit)
	} else {
		var role string
		err = s.db.QueryRowContext(ctx, `SELECT membership_role FROM collection_memberships WHERE tenant_id=$1 AND collection_id=$2 AND principal_id=$3`, identity.TenantID, collectionID, identity.UserID).Scan(&role)
		if err != nil || !canReview(role) {
			return nil, ErrForbidden
		}
		rows, err = s.db.QueryContext(ctx, `SELECT id::text,actor_type,actor_id,action,resource_type,resource_id,COALESCE(version_id::text,''),outcome,rationale,details,created_at
		FROM audit_events WHERE tenant_id=$1 AND collection_id=$2 ORDER BY created_at DESC LIMIT $3`, identity.TenantID, collectionID, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Activity{}
	for rows.Next() {
		var item Activity
		if err := rows.Scan(&item.ID, &item.ActorType, &item.ActorID, &item.Action, &item.ResourceType, &item.ResourceID, &item.VersionID, &item.Outcome, &item.Rationale, &item.Details, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func loadTarget(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, tenantID, documentID, versionID string, lock bool) (target, error) {
	var t target
	query := `SELECT d.id::text,COALESCE(v.id::text,''),d.collection_id::text,COALESCE(d.case_id::text,''),d.created_by::text,COALESCE(v.created_by::text,''),COALESCE(d.published_version_id::text,''),COALESCE(d.current_version_id::text,''),d.deleted_at,d.revoked_at,v.revoked_at,v.expires_at,COALESCE(v.parser_status,''),COALESCE(v.index_status,''),COALESCE(v.review_status,''),COALESCE(v.trust_status,''),COALESCE(v.publication_status,''),COALESCE(v.version_number,0),COALESCE(v.checksum_sha256,''),COALESCE(v.created_at,d.created_at)
	FROM documents d `
	if versionID != "" {
		query += `JOIN document_versions v ON v.tenant_id=d.tenant_id AND v.document_id=d.id AND v.id=$3::uuid `
	} else {
		query += `LEFT JOIN LATERAL (SELECT * FROM document_versions current WHERE current.tenant_id=d.tenant_id AND current.document_id=d.id ORDER BY current.version_number DESC LIMIT 1) v ON true `
	}
	query += `WHERE d.tenant_id=$1 AND d.id=$2`
	args := []any{tenantID, documentID}
	if versionID != "" {
		args = append(args, versionID)
	}
	if lock {
		query += ` FOR UPDATE OF d`
		if versionID != "" {
			query += `,v`
		}
	}
	err := q.QueryRowContext(ctx, query, args...).Scan(&t.DocumentID, &t.VersionID, &t.CollectionID, &t.CaseID, &t.DocumentAuthor, &t.VersionAuthor, &t.PublishedVersion, &t.CurrentVersion, &t.DeletedAt, &t.RevokedAt, &t.VersionRevokedAt, &t.ExpiresAt, &t.ParserStatus, &t.IndexStatus, &t.ReviewStatus, &t.TrustStatus, &t.Publication, &t.VersionNumber, &t.Checksum, &t.CreatedAt)
	return t, err
}

func membershipRole(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, identity auth.Identity, t target) (string, error) {
	var role string
	var err error
	if t.CaseID != "" {
		err = q.QueryRowContext(ctx, `SELECT membership_role FROM case_memberships WHERE tenant_id=$1 AND case_id=$2 AND principal_id=$3`, identity.TenantID, t.CaseID, identity.UserID).Scan(&role)
	} else {
		err = q.QueryRowContext(ctx, `SELECT membership_role FROM collection_memberships WHERE tenant_id=$1 AND collection_id=$2 AND principal_id=$3`, identity.TenantID, t.CollectionID, identity.UserID).Scan(&role)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrForbidden
	}
	return role, err
}

func ensureLive(t target) error {
	if err := ensureDocumentLive(t); err != nil || t.VersionRevokedAt.Valid || (t.ExpiresAt.Valid && !t.ExpiresAt.Time.After(time.Now())) {
		return ErrNotFound
	}
	return nil
}

func ensureDocumentLive(t target) error {
	if t.DeletedAt.Valid || t.RevokedAt.Valid {
		return ErrNotFound
	}
	return nil
}

func canContribute(role string) bool {
	return role == "owner" || role == "steward" || role == "contributor"
}
func canReview(role string) bool  { return role == "owner" || role == "steward" || role == "reviewer" }
func canSteward(role string) bool { return role == "owner" || role == "steward" }

// reviewAuditDetails marks a decision taken under the development self-approval exemption.
func reviewAuditDetails(reviewID string, selfApprovalExempt bool) map[string]any {
	details := map[string]any{"review_id": reviewID}
	if selfApprovalExempt {
		details["self_approval_exempt"] = true
	}
	return details
}

func selfApprovalDenied(actorID, submittedBy, versionAuthor string) bool {
	return actorID != "" && (actorID == submittedBy || actorID == versionAuthor)
}

func appendAudit(ctx context.Context, tx *sql.Tx, tenantID, actorType, actorID, action, resourceType, resourceID, collectionID, caseID, versionID, outcome, rationale, correlationID string, details any) error {
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,collection_id,case_id,version_id,outcome,rationale,correlation_id,details)
	VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,'')::uuid,NULLIF($8,'')::uuid,NULLIF($9,'')::uuid,$10,$11,$12,$13::jsonb)`, tenantID, actorType, actorID, action, resourceType, resourceID, collectionID, caseID, versionID, outcome, rationale, correlationID, string(data))
	return err
}

func enqueueOutbox(ctx context.Context, tx *sql.Tx, tenantID, eventType, aggregateID string, payload any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox_events(tenant_id,event_type,aggregate_id,payload) VALUES($1,$2,$3,$4::jsonb)`, tenantID, eventType, aggregateID, string(data))
	return err
}

func mapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}
