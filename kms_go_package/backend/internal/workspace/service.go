package workspace

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/elenio/knowledge-workspace/backend/internal/auth"
	"github.com/elenio/knowledge-workspace/backend/internal/platform"
)

var (
	ErrForbidden      = errors.New("resource access denied")
	ErrNotFound       = errors.New("resource not found")
	ErrStorageMissing = errors.New("object storage is not configured")
	ErrConflict       = errors.New("resource already exists")
	uuidPattern       = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-[1-8][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
)

type Service struct {
	db          *sql.DB
	store       platform.ObjectStore
	maxUpload   int64
	maxJobTries int
}

func NewService(db *sql.DB, store platform.ObjectStore, maxUpload int64) *Service {
	return &Service{db: db, store: store, maxUpload: maxUpload, maxJobTries: 5}
}

type Collection struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Classification string    `json:"classification"`
	DocumentCount  int       `json:"document_count"`
	CreatedAt      time.Time `json:"created_at"`
}

type CaseScope struct {
	ID           string    `json:"id"`
	CollectionID string    `json:"collection_id"`
	Name         string    `json:"name"`
	CreatedAt    time.Time `json:"created_at"`
}

type Document struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	CollectionID    string    `json:"collection_id"`
	LifecycleStatus string    `json:"lifecycle_status"`
	VersionID       string    `json:"version_id,omitempty"`
	VersionNumber   int       `json:"version_number,omitempty"`
	ParserStatus    string    `json:"parser_status,omitempty"`
	IndexStatus     string    `json:"index_status,omitempty"`
	JobID           string    `json:"job_id,omitempty"`
	JobStatus       string    `json:"job_status,omitempty"`
	JobStage        string    `json:"job_stage,omitempty"`
	LastError       string    `json:"last_error,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

type Job struct {
	ID         string    `json:"id"`
	DocumentID string    `json:"document_id"`
	VersionID  string    `json:"version_id"`
	Status     string    `json:"status"`
	Stage      string    `json:"stage"`
	Attempts   int       `json:"attempts"`
	LastError  string    `json:"last_error,omitempty"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (s *Service) CreateCollection(ctx context.Context, identity auth.Identity, name, description, classification string) (Collection, error) {
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) {
		return Collection{}, fmt.Errorf("verified identity must use UUID tenant and user IDs")
	}
	if !hasRole(identity, "platform_admin", "knowledge_steward") {
		return Collection{}, ErrForbidden
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 160 {
		return Collection{}, fmt.Errorf("collection name must contain 1 to 160 characters")
	}
	if classification == "" {
		classification = "internal"
	}
	if classification != "internal" && classification != "confidential" && classification != "restricted" {
		return Collection{}, fmt.Errorf("classification must be internal, confidential, or restricted")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Collection{}, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO tenants(id,name) VALUES($1,'Workspace') ON CONFLICT(id) DO NOTHING`, identity.TenantID); err != nil {
		return Collection{}, err
	}
	var collection Collection
	err = tx.QueryRowContext(ctx, `INSERT INTO collections(tenant_id,name,description,classification) VALUES($1,$2,$3,$4) RETURNING id::text, name, description, classification, created_at`, identity.TenantID, name, strings.TrimSpace(description), classification).Scan(&collection.ID, &collection.Name, &collection.Description, &collection.Classification, &collection.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return Collection{}, ErrConflict
		}
		return Collection{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO collection_memberships(tenant_id,collection_id,principal_id,membership_role,granted_by) VALUES($1,$2,$3,'owner',$3)`, identity.TenantID, collection.ID, identity.UserID); err != nil {
		return Collection{}, err
	}
	if err := tx.Commit(); err != nil {
		return Collection{}, err
	}
	return collection, nil
}

func (s *Service) AddCollectionMember(ctx context.Context, identity auth.Identity, collectionID, principalID, role string) error {
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) || !validUUID(collectionID) || !validUUID(principalID) {
		return fmt.Errorf("verified identity and collection/member IDs must use UUIDs")
	}
	if role != "reader" && role != "contributor" && role != "reviewer" && role != "steward" {
		return fmt.Errorf("membership role must be reader, contributor, reviewer, or steward")
	}
	var actorRole string
	err := s.db.QueryRowContext(ctx, `SELECT membership_role FROM collection_memberships WHERE tenant_id=$1 AND collection_id=$2 AND principal_id=$3`, identity.TenantID, collectionID, identity.UserID).Scan(&actorRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if actorRole != "owner" && actorRole != "steward" {
		return ErrForbidden
	}
	if role == "steward" && actorRole != "owner" {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO collection_memberships(tenant_id,collection_id,principal_id,membership_role,granted_by)
	VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,collection_id,principal_id) DO UPDATE SET membership_role=EXCLUDED.membership_role`, identity.TenantID, collectionID, principalID, role, identity.UserID); err != nil {
		return err
	}
	if err := recordMembershipGrant(ctx, tx, identity, collectionID, "", principalID, role); err != nil {
		return err
	}
	return tx.Commit()
}

// recordMembershipGrant audits the grant and queues an ACL projection refresh, so chunks indexed
// before the grant become discoverable; PostgreSQL still authorizes every hit.
func recordMembershipGrant(ctx context.Context, tx *sql.Tx, identity auth.Identity, collectionID, caseID, principalID, role string) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,collection_id,case_id,outcome,details)
	VALUES($1,'user',$2,'membership.grant','principal',$3::text,$4::uuid,NULLIF($5::text,'')::uuid,'allowed',jsonb_build_object('role',$6::text))`, identity.TenantID, identity.UserID, principalID, collectionID, caseID, role); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO outbox_events(tenant_id,event_type,aggregate_id,payload) VALUES($1,'scope.acl_refresh',$2::uuid,jsonb_build_object('collection_id',$2::text))`, identity.TenantID, collectionID)
	return err
}

func (s *Service) CreateCase(ctx context.Context, identity auth.Identity, collectionID, name string) (CaseScope, error) {
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) || !validUUID(collectionID) {
		return CaseScope{}, fmt.Errorf("verified identity and collection ID must use UUIDs")
	}
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 160 {
		return CaseScope{}, fmt.Errorf("case name must contain 1 to 160 characters")
	}
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT membership_role FROM collection_memberships WHERE tenant_id=$1 AND collection_id=$2 AND principal_id=$3`, identity.TenantID, collectionID, identity.UserID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return CaseScope{}, ErrForbidden
	}
	if err != nil {
		return CaseScope{}, err
	}
	if role != "owner" && role != "steward" && role != "contributor" {
		return CaseScope{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return CaseScope{}, err
	}
	defer tx.Rollback()
	var scope CaseScope
	err = tx.QueryRowContext(ctx, `INSERT INTO case_scopes(tenant_id,collection_id,name,created_by) VALUES($1,$2,$3,$4) RETURNING case_id::text,collection_id::text,name,created_at`, identity.TenantID, collectionID, name, identity.UserID).Scan(&scope.ID, &scope.CollectionID, &scope.Name, &scope.CreatedAt)
	if err != nil {
		return CaseScope{}, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO case_memberships(tenant_id,case_id,principal_id,membership_role,granted_by) VALUES($1,$2,$3,'owner',$3)`, identity.TenantID, scope.ID, identity.UserID); err != nil {
		return CaseScope{}, err
	}
	return scope, tx.Commit()
}

func (s *Service) AddCaseMember(ctx context.Context, identity auth.Identity, caseID, principalID, role string) error {
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) || !validUUID(caseID) || !validUUID(principalID) {
		return fmt.Errorf("verified identity and case/member IDs must use UUIDs")
	}
	if role != "reader" && role != "contributor" && role != "reviewer" && role != "steward" {
		return fmt.Errorf("membership role must be reader, contributor, reviewer, or steward")
	}
	var caseRole string
	err := s.db.QueryRowContext(ctx, `SELECT membership_role FROM case_memberships WHERE tenant_id=$1 AND case_id=$2 AND principal_id=$3`, identity.TenantID, caseID, identity.UserID).Scan(&caseRole)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrForbidden
	}
	if err != nil {
		return err
	}
	if caseRole != "owner" && caseRole != "steward" {
		return ErrForbidden
	}
	if role == "steward" && caseRole != "owner" {
		return ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var collectionID string
	if err := tx.QueryRowContext(ctx, `SELECT collection_id::text FROM case_scopes WHERE tenant_id=$1 AND case_id=$2`, identity.TenantID, caseID).Scan(&collectionID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO collection_memberships(tenant_id,collection_id,principal_id,membership_role,granted_by) VALUES($1,$2,$3,'reader',$4) ON CONFLICT(tenant_id,collection_id,principal_id) DO NOTHING`, identity.TenantID, collectionID, principalID, identity.UserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO case_memberships(tenant_id,case_id,principal_id,membership_role,granted_by) VALUES($1,$2,$3,$4,$5)
	ON CONFLICT(tenant_id,case_id,principal_id) DO UPDATE SET membership_role=EXCLUDED.membership_role`, identity.TenantID, caseID, principalID, role, identity.UserID); err != nil {
		return err
	}
	if err := recordMembershipGrant(ctx, tx, identity, collectionID, caseID, principalID, role); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) ListCollections(ctx context.Context, identity auth.Identity) ([]Collection, error) {
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) {
		return nil, fmt.Errorf("verified identity must use UUID tenant and user IDs")
	}
	rows, err := s.db.QueryContext(ctx, `SELECT c.id::text,c.name,c.description,c.classification,COUNT(d.id)::int,c.created_at
	FROM collections c JOIN collection_memberships m ON m.tenant_id=c.tenant_id AND m.collection_id=c.id
	LEFT JOIN documents d ON d.tenant_id=c.tenant_id AND d.collection_id=c.id
	WHERE c.tenant_id=$1 AND m.principal_id=$2 GROUP BY c.id ORDER BY c.name`, identity.TenantID, identity.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Collection{}
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.ID, &item.Name, &item.Description, &item.Classification, &item.DocumentCount, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) ListDocuments(ctx context.Context, identity auth.Identity) ([]Document, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id::text,d.title,d.collection_id::text,d.lifecycle_status,d.created_at,
	COALESCE(v.id::text,''),COALESCE(v.version_number,0),COALESCE(v.parser_status,''),COALESCE(v.index_status,''),
	COALESCE(j.id::text,''),COALESCE(j.status,''),COALESCE(j.stage,''),CASE WHEN j.status='failed' THEN 'Processing failed; retry or contact a steward.' ELSE '' END
	FROM documents d
	JOIN collection_memberships m ON m.tenant_id=d.tenant_id AND m.collection_id=d.collection_id AND m.principal_id=$2
	LEFT JOIN LATERAL (SELECT * FROM document_versions WHERE tenant_id=d.tenant_id AND document_id=d.id AND id=d.current_version_id ORDER BY version_number DESC LIMIT 1) v ON true
	LEFT JOIN LATERAL (SELECT * FROM ingestion_jobs WHERE tenant_id=d.tenant_id AND document_id=d.id ORDER BY created_at DESC LIMIT 1) j ON true
	WHERE d.tenant_id=$1 AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND (d.case_id IS NULL OR EXISTS(SELECT 1 FROM case_memberships cm WHERE cm.tenant_id=d.tenant_id AND cm.case_id=d.case_id AND cm.principal_id=$2))
	ORDER BY d.created_at DESC LIMIT 500`, identity.TenantID, identity.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Document{}
	for rows.Next() {
		var item Document
		if err := rows.Scan(&item.ID, &item.Title, &item.CollectionID, &item.LifecycleStatus, &item.CreatedAt, &item.VersionID, &item.VersionNumber, &item.ParserStatus, &item.IndexStatus, &item.JobID, &item.JobStatus, &item.JobStage, &item.LastError); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (s *Service) UploadPDF(ctx context.Context, identity auth.Identity, collectionID, caseID, title, filename string, content []byte) (Document, Job, error) {
	if s.store == nil {
		return Document{}, Job{}, ErrStorageMissing
	}
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) || !validUUID(collectionID) {
		return Document{}, Job{}, fmt.Errorf("verified identity and collection ID must use UUIDs")
	}
	if int64(len(content)) > s.maxUpload {
		return Document{}, Job{}, fmt.Errorf("PDF exceeds the configured maximum upload size")
	}
	if len(content) < 5 || string(content[:5]) != "%PDF-" {
		return Document{}, Job{}, fmt.Errorf("uploaded content is not a PDF")
	}
	filename = filepathBase(filename)
	title = strings.TrimSpace(title)
	if title == "" {
		title = strings.TrimSuffix(filename, ".pdf")
	}
	if title == "" || len(title) > 240 {
		return Document{}, Job{}, fmt.Errorf("document title must contain 1 to 240 characters")
	}
	var role string
	err := s.db.QueryRowContext(ctx, `SELECT membership_role FROM collection_memberships WHERE tenant_id=$1 AND collection_id=$2 AND principal_id=$3`, identity.TenantID, collectionID, identity.UserID).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, Job{}, ErrForbidden
	}
	if err != nil {
		return Document{}, Job{}, err
	}
	collectionCanContribute := canContribute(role)
	if caseID != "" {
		if !validUUID(caseID) {
			return Document{}, Job{}, fmt.Errorf("case ID must be a UUID")
		}
		var caseRole string
		err := s.db.QueryRowContext(ctx, `SELECT cm.membership_role FROM case_scopes cs JOIN case_memberships cm ON cm.tenant_id=cs.tenant_id AND cm.case_id=cs.case_id
		WHERE cs.tenant_id=$1 AND cs.case_id=$2 AND cs.collection_id=$3 AND cm.principal_id=$4`, identity.TenantID, caseID, collectionID, identity.UserID).Scan(&caseRole)
		if errors.Is(err, sql.ErrNoRows) {
			return Document{}, Job{}, ErrForbidden
		}
		if err != nil {
			return Document{}, Job{}, err
		}
		if !canUpload(role, caseRole) {
			return Document{}, Job{}, ErrForbidden
		}
	} else if !collectionCanContribute {
		return Document{}, Job{}, ErrForbidden
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, Job{}, err
	}
	defer tx.Rollback()
	var documentID, versionID, jobID string
	if err := tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&documentID, &versionID, &jobID); err != nil {
		return Document{}, Job{}, err
	}
	checksum := sha256.Sum256(content)
	objectKey := fmt.Sprintf("%s/documents/%s/versions/%s/original.pdf", identity.TenantID, documentID, versionID)
	if err := s.store.Put(ctx, objectKey, "application/pdf", content); err != nil {
		return Document{}, Job{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.store.Delete(context.Background(), objectKey)
		}
	}()
	var result Document
	result.ID, result.Title, result.CollectionID = documentID, title, collectionID
	result.LifecycleStatus = "processing"
	result.CreatedAt = time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `INSERT INTO documents(id,tenant_id,collection_id,case_id,title,created_by,lifecycle_status) VALUES($1,$2,$3,NULLIF($4,'')::uuid,$5,$6,'processing')`, documentID, identity.TenantID, collectionID, caseID, title, identity.UserID); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO document_versions(id,tenant_id,document_id,version_number,checksum_sha256,original_object_key,original_filename,media_type,parser_status,index_status,review_status,created_by,trust_status,publication_status) VALUES($1,$2,$3,1,$4,$5,$6,'application/pdf','pending','pending','draft',$7,CASE WHEN NULLIF($8,'') IS NULL THEN 'unverified' ELSE 'customer_source' END,'draft')`, versionID, identity.TenantID, documentID, hex.EncodeToString(checksum[:]), objectKey, filename, identity.UserID, caseID); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET current_version_id=$3 WHERE tenant_id=$1 AND id=$2`, identity.TenantID, documentID, versionID); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ingestion_jobs(id,tenant_id,collection_id,document_id,version_id,max_attempts) VALUES($1,$2,$3,$4,$5,$6)`, jobID, identity.TenantID, collectionID, documentID, versionID, s.maxJobTries); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,collection_id,case_id,version_id,outcome,details)
		VALUES($1,'user',$2,'document.version.upload','document_version',$3::text,$4,NULLIF($5,'')::uuid,$3::uuid,'allowed',jsonb_build_object('version_number',1))`, identity.TenantID, identity.UserID, versionID, collectionID, caseID); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO knowledge_sources(tenant_id,collection_id,document_id,source_uri,source_type,trust_status) VALUES($1,$2,$3,$4,'upload',CASE WHEN NULLIF($5,'') IS NULL THEN 'unverified' ELSE 'customer_source' END)`, identity.TenantID, collectionID, documentID, "kms://"+documentID+"/"+versionID, caseID); err != nil {
		return Document{}, Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Document{}, Job{}, err
	}
	committed = true
	result.VersionID, result.VersionNumber, result.ParserStatus, result.IndexStatus, result.JobID, result.JobStatus, result.JobStage = versionID, 1, "pending", "pending", jobID, "queued", "queued"
	return result, Job{ID: jobID, DocumentID: documentID, VersionID: versionID, Status: "queued", Stage: "queued", Attempts: 0, UpdatedAt: result.CreatedAt}, nil
}

// UploadVersion creates an immutable source version. The document row lock serializes
// version-number allocation while leaving any previously published version untouched.
func (s *Service) UploadVersion(ctx context.Context, identity auth.Identity, documentID, filename string, content []byte) (Document, Job, error) {
	if s.store == nil {
		return Document{}, Job{}, ErrStorageMissing
	}
	if !validUUID(identity.TenantID) || !validUUID(identity.UserID) || !validUUID(documentID) {
		return Document{}, Job{}, fmt.Errorf("verified identity and document IDs must use UUIDs")
	}
	if int64(len(content)) > s.maxUpload {
		return Document{}, Job{}, fmt.Errorf("PDF exceeds the configured maximum upload size")
	}
	if len(content) < 5 || string(content[:5]) != "%PDF-" {
		return Document{}, Job{}, fmt.Errorf("uploaded content is not a PDF")
	}
	filename = filepathBase(filename)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Document{}, Job{}, err
	}
	defer tx.Rollback()
	var collectionID, caseID, title string
	var deleted, revoked bool
	var publishedVersion sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT collection_id::text,COALESCE(case_id::text,''),title,deleted_at IS NOT NULL,revoked_at IS NOT NULL,published_version_id::text
	FROM documents WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, identity.TenantID, documentID).Scan(&collectionID, &caseID, &title, &deleted, &revoked, &publishedVersion)
	if errors.Is(err, sql.ErrNoRows) {
		return Document{}, Job{}, ErrNotFound
	}
	if err != nil {
		return Document{}, Job{}, err
	}
	if deleted || revoked {
		return Document{}, Job{}, ErrForbidden
	}
	var role string
	if caseID != "" {
		err = tx.QueryRowContext(ctx, `SELECT membership_role FROM case_memberships WHERE tenant_id=$1 AND case_id=$2 AND principal_id=$3`, identity.TenantID, caseID, identity.UserID).Scan(&role)
	} else {
		err = tx.QueryRowContext(ctx, `SELECT membership_role FROM collection_memberships WHERE tenant_id=$1 AND collection_id=$2 AND principal_id=$3`, identity.TenantID, collectionID, identity.UserID).Scan(&role)
	}
	if errors.Is(err, sql.ErrNoRows) || !canContribute(role) {
		return Document{}, Job{}, ErrForbidden
	}
	if err != nil {
		return Document{}, Job{}, err
	}
	var versionNumber int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version_number),0)+1 FROM document_versions WHERE tenant_id=$1 AND document_id=$2`, identity.TenantID, documentID).Scan(&versionNumber); err != nil {
		return Document{}, Job{}, err
	}
	var versionID, jobID string
	if err := tx.QueryRowContext(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&versionID, &jobID); err != nil {
		return Document{}, Job{}, err
	}
	checksum := sha256.Sum256(content)
	objectKey := fmt.Sprintf("%s/documents/%s/versions/%s/original.pdf", identity.TenantID, documentID, versionID)
	if err := s.store.Put(ctx, objectKey, "application/pdf", content); err != nil {
		return Document{}, Job{}, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = s.store.Delete(context.Background(), objectKey)
		}
	}()
	if _, err := tx.ExecContext(ctx, `INSERT INTO document_versions(id,tenant_id,document_id,version_number,checksum_sha256,original_object_key,original_filename,media_type,parser_status,index_status,review_status,created_by,trust_status,publication_status)
	VALUES($1,$2,$3,$4,$5,$6,$7,'application/pdf','pending','pending','draft',$8,CASE WHEN NULLIF($9,'') IS NULL THEN 'unverified' ELSE 'customer_source' END,'draft')`, versionID, identity.TenantID, documentID, versionNumber, hex.EncodeToString(checksum[:]), objectKey, filename, identity.UserID, caseID); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO ingestion_jobs(id,tenant_id,collection_id,document_id,version_id,max_attempts) VALUES($1,$2,$3,$4,$5,$6)`, jobID, identity.TenantID, collectionID, documentID, versionID, s.maxJobTries); err != nil {
		return Document{}, Job{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,collection_id,case_id,version_id,outcome,details)
	VALUES($1,'user',$2,'document.version.upload','document_version',$3::text,$4,NULLIF($5,'')::uuid,$3::uuid,'allowed',jsonb_build_object('version_number',$6::int))`, identity.TenantID, identity.UserID, versionID, collectionID, caseID, versionNumber); err != nil {
		return Document{}, Job{}, err
	}
	status := "processing"
	if publishedVersion.Valid {
		status = "published"
	}
	if _, err := tx.ExecContext(ctx, `UPDATE documents SET current_version_id=$3,lifecycle_status=$4,updated_at=now() WHERE tenant_id=$1 AND id=$2`, identity.TenantID, documentID, versionID, status); err != nil {
		return Document{}, Job{}, err
	}
	if err := tx.Commit(); err != nil {
		return Document{}, Job{}, err
	}
	committed = true
	now := time.Now().UTC()
	item := Document{ID: documentID, Title: title, CollectionID: collectionID, LifecycleStatus: status, VersionID: versionID, VersionNumber: versionNumber, ParserStatus: "pending", IndexStatus: "pending", JobID: jobID, JobStatus: "queued", JobStage: "queued", CreatedAt: now}
	return item, Job{ID: jobID, DocumentID: documentID, VersionID: versionID, Status: "queued", Stage: "queued", UpdatedAt: now}, nil
}

func (s *Service) GetJob(ctx context.Context, identity auth.Identity, jobID string) (Job, error) {
	var job Job
	err := s.db.QueryRowContext(ctx, `SELECT j.id::text,j.document_id::text,j.version_id::text,j.status,j.stage,j.attempts,CASE WHEN j.status='failed' THEN 'Processing failed; retry or contact a steward.' ELSE '' END,j.updated_at
	FROM ingestion_jobs j JOIN collection_memberships m ON m.tenant_id=j.tenant_id AND m.collection_id=j.collection_id
	JOIN documents d ON d.tenant_id=j.tenant_id AND d.id=j.document_id
	JOIN document_versions v ON v.tenant_id=j.tenant_id AND v.id=j.version_id
	WHERE j.tenant_id=$1 AND j.id=$2 AND m.principal_id=$3 AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	AND (d.case_id IS NULL OR EXISTS(SELECT 1 FROM case_memberships cm WHERE cm.tenant_id=d.tenant_id AND cm.case_id=d.case_id AND cm.principal_id=$3))`, identity.TenantID, jobID, identity.UserID).Scan(&job.ID, &job.DocumentID, &job.VersionID, &job.Status, &job.Stage, &job.Attempts, &job.LastError, &job.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	return job, err
}

func (s *Service) RetryJob(ctx context.Context, identity auth.Identity, jobID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE ingestion_jobs j SET status='queued',stage='queued',attempts=0,available_at=now(),lease_expires_at=NULL,last_error=NULL,updated_at=now()
	WHERE j.tenant_id=$1 AND j.id=$2 AND j.status='failed'
	AND EXISTS (SELECT 1 FROM collection_memberships m WHERE m.tenant_id=j.tenant_id AND m.collection_id=j.collection_id AND m.principal_id=$3)
	AND EXISTS (SELECT 1 FROM documents d JOIN document_versions v ON v.tenant_id=d.tenant_id AND v.document_id=d.id WHERE d.tenant_id=j.tenant_id AND d.id=j.document_id AND v.id=j.version_id AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND v.review_status IN ('draft','rejected') AND (v.expires_at IS NULL OR v.expires_at>now()) AND (
		(d.case_id IS NULL AND EXISTS(SELECT 1 FROM collection_memberships m WHERE m.tenant_id=d.tenant_id AND m.collection_id=d.collection_id AND m.principal_id=$3 AND m.membership_role IN ('owner','steward','contributor')))
		OR (d.case_id IS NOT NULL AND EXISTS(SELECT 1 FROM case_memberships cm WHERE cm.tenant_id=d.tenant_id AND cm.case_id=d.case_id AND cm.principal_id=$3 AND cm.membership_role IN ('owner','steward','contributor')))
	))`, identity.TenantID, jobID, identity.UserID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	if _, err = tx.ExecContext(ctx, `UPDATE document_versions v SET parser_status='pending',index_status='pending' FROM ingestion_jobs j WHERE j.tenant_id=v.tenant_id AND j.version_id=v.id AND j.tenant_id=$1 AND j.id=$2`, identity.TenantID, jobID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE documents d SET lifecycle_status='processing',updated_at=now() FROM ingestion_jobs j WHERE j.tenant_id=d.tenant_id AND j.document_id=d.id AND j.tenant_id=$1 AND j.id=$2`, identity.TenantID, jobID); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) OriginalPDF(ctx context.Context, identity auth.Identity, documentID, versionID string) ([]byte, error) {
	if s.store == nil {
		return nil, ErrStorageMissing
	}
	var objectKey string
	var caseID, collectionRole, caseRole, reviewStatus, trustStatus, publicationStatus, parserStatus, indexStatus string
	var currentPublished bool
	err := s.db.QueryRowContext(ctx, `SELECT v.original_object_key,COALESCE(d.case_id::text,''),m.membership_role,COALESCE(casem.membership_role,''),
	(d.published_version_id=v.id),v.review_status,v.trust_status,v.publication_status,v.parser_status,v.index_status FROM document_versions v
	JOIN documents d ON d.tenant_id=v.tenant_id AND d.id=v.document_id
	JOIN collection_memberships m ON m.tenant_id=d.tenant_id AND m.collection_id=d.collection_id
	LEFT JOIN case_memberships casem ON casem.tenant_id=d.tenant_id AND casem.case_id=d.case_id AND casem.principal_id=m.principal_id
	WHERE d.tenant_id=$1 AND d.id=$2 AND v.id=$3 AND m.principal_id=$4 AND d.deleted_at IS NULL AND d.revoked_at IS NULL AND v.revoked_at IS NULL AND (v.expires_at IS NULL OR v.expires_at>now())
	AND (d.case_id IS NULL OR casem.principal_id=$4)`, identity.TenantID, documentID, versionID, identity.UserID).
		Scan(&objectKey, &caseID, &collectionRole, &caseRole, &currentPublished, &reviewStatus, &trustStatus, &publicationStatus, &parserStatus, &indexStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	if !canPreviewVersion(caseID != "", collectionRole, caseRole, currentPublished, reviewStatus, trustStatus, publicationStatus, parserStatus, indexStatus) {
		return nil, ErrNotFound
	}
	return s.store.Get(ctx, objectKey)
}

func validUUID(value string) bool { return uuidPattern.MatchString(value) }

func hasRole(identity auth.Identity, roles ...string) bool {
	for _, current := range identity.Roles {
		for _, allowed := range roles {
			if current == allowed {
				return true
			}
		}
	}
	return false
}

func canContribute(role string) bool {
	return role == "owner" || role == "steward" || role == "contributor"
}

func canPreviewVersion(caseScoped bool, collectionRole, caseRole string, currentPublished bool, review, trust, publication, parser, index string) bool {
	if caseScoped {
		if canReviewOrContribute(caseRole) {
			return true
		}
		if caseRole != "" && trust == "customer_source" && parser == "success" && index == "indexed" {
			return true
		}
	} else if canReviewOrContribute(collectionRole) {
		return true
	}
	return currentPublished && publication == "published" && review == "approved" && trust == "approved" && parser == "success" && index == "indexed"
}

func canReviewOrContribute(role string) bool {
	return canContribute(role) || role == "reviewer"
}

func canUpload(collectionRole, caseRole string) bool {
	if caseRole != "" {
		return canContribute(caseRole)
	}
	return canContribute(collectionRole)
}

func filepathBase(name string) string {
	name = strings.ReplaceAll(name, `\`, "/")
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return "document.pdf"
	}
	return parts[len(parts)-1]
}
