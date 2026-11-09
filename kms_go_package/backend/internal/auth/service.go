package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

var ErrServiceUnauthenticated = errors.New("service credential is invalid, expired, or revoked")

const RetrievalAudience = "kms-retrieval"

type ResourceGrant struct {
	CollectionID string `json:"collection_id"`
	CaseID       string `json:"case_id,omitempty"`
}

type ServiceIdentity struct {
	ClientID  string
	TenantID  string
	Audience  string
	Scopes    []string
	Grants    []ResourceGrant
	ExpiresAt time.Time
}

type ServiceAuthenticator struct{ db *sql.DB }

func NewServiceAuthenticator(db *sql.DB) *ServiceAuthenticator {
	return &ServiceAuthenticator{db: db}
}

func (a *ServiceAuthenticator) Authenticate(ctx context.Context, request *http.Request, audience string) (ServiceIdentity, error) {
	if a == nil || a.db == nil {
		return ServiceIdentity{}, ErrServiceUnauthenticated
	}
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || len(parts[1]) < 40 {
		return ServiceIdentity{}, ErrServiceUnauthenticated
	}
	hash := CredentialHash(parts[1])
	var identity ServiceIdentity
	var rawScopes, rawGrants []byte
	var revokedAt sql.NullTime
	err := a.db.QueryRowContext(ctx, `SELECT c.id::text,c.tenant_id::text,c.audience,array_to_json(c.scopes),c.expires_at,c.revoked_at,
	COALESCE(json_agg(json_build_object('collection_id',g.collection_id::text,'case_id',g.case_id::text)) FILTER (WHERE g.id IS NOT NULL),'[]'::json)
	FROM service_clients c LEFT JOIN service_client_grants g ON g.tenant_id=c.tenant_id AND g.client_id=c.id
	WHERE c.token_hash=$1 GROUP BY c.id`, hash).Scan(&identity.ClientID, &identity.TenantID, &identity.Audience, &rawScopes, &identity.ExpiresAt, &revokedAt, &rawGrants)
	if errors.Is(err, sql.ErrNoRows) {
		return ServiceIdentity{}, ErrServiceUnauthenticated
	}
	if err != nil {
		return ServiceIdentity{}, err
	}
	return decodeServiceRecord(identity, rawScopes, rawGrants, audience, revokedAt.Valid, time.Now())
}

// decodeServiceRecord fills scopes and grants before validation; checking first would reject every credential.
func decodeServiceRecord(identity ServiceIdentity, rawScopes, rawGrants []byte, audience string, revoked bool, now time.Time) (ServiceIdentity, error) {
	if err := json.Unmarshal(rawScopes, &identity.Scopes); err != nil {
		return ServiceIdentity{}, fmt.Errorf("decode service scopes: %w", err)
	}
	if err := json.Unmarshal(rawGrants, &identity.Grants); err != nil {
		return ServiceIdentity{}, fmt.Errorf("decode service grants: %w", err)
	}
	if !serviceRecordValid(identity, audience, revoked, now) {
		return ServiceIdentity{}, ErrServiceUnauthenticated
	}
	return identity, nil
}

func CredentialHash(token string) string {
	digest := sha256.Sum256([]byte(token))
	return hex.EncodeToString(digest[:])
}

// ServiceClientScopes returns the scopes for a new retrieval service client. Every client holds
// retrieval:query; delegate adds retrieval:delegate (act for a named tenant principal via DelegatedUserHeader).
func ServiceClientScopes(delegate bool) []string {
	if delegate {
		return []string{ScopeRetrievalQuery, ScopeRetrievalDelegate}
	}
	return []string{ScopeRetrievalQuery}
}

func validServiceClientScopes(scopes []string) bool {
	hasQuery := false
	for _, scope := range scopes {
		switch scope {
		case ScopeRetrievalQuery:
			hasQuery = true
		case ScopeRetrievalDelegate:
		default:
			return false
		}
	}
	return hasQuery
}

// ProvisionServiceClient generates a high-entropy bearer credential. The returned
// token is the only copy of the secret; only its SHA-256 digest is persisted.
// scopes must include retrieval:query and may add retrieval:delegate (see ServiceClientScopes).
func ProvisionServiceClient(ctx context.Context, db *sql.DB, tenantID, actorID, name string, expiresAt time.Time, grants []ResourceGrant, scopes []string) (string, string, error) {
	name = strings.TrimSpace(name)
	if db == nil || name == "" || len(name) > 160 || !expiresAt.After(time.Now()) || len(grants) == 0 {
		return "", "", fmt.Errorf("service client requires a database, name, future expiry, and at least one resource grant")
	}
	if !validServiceClientScopes(scopes) {
		return "", "", fmt.Errorf("service client scopes must include %s and may only add %s", ScopeRetrievalQuery, ScopeRetrievalDelegate)
	}
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", "", err
	}
	token := "kms_svc_" + base64.RawURLEncoding.EncodeToString(secret[:])
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return "", "", err
	}
	defer tx.Rollback()
	var id string
	err = tx.QueryRowContext(ctx, `INSERT INTO service_clients(tenant_id,name,token_hash,audience,scopes,expires_at,created_by)
	VALUES($1,$2,$3,$4,$5::text[],$6,$7) RETURNING id::text`, tenantID, name, CredentialHash(token), RetrievalAudience, scopes, expiresAt, actorID).Scan(&id)
	if err != nil {
		return "", "", err
	}
	seen := map[string]struct{}{}
	for _, grant := range grants {
		if !isUUID(grant.CollectionID) || (grant.CaseID != "" && !isUUID(grant.CaseID)) {
			return "", "", fmt.Errorf("service grant IDs must be UUIDs")
		}
		key := grant.CollectionID + ":" + grant.CaseID
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		if _, err := tx.ExecContext(ctx, `INSERT INTO service_client_grants(tenant_id,client_id,collection_id,case_id) VALUES($1,$2,$3,NULLIF($4,'')::uuid)`, tenantID, id, grant.CollectionID, grant.CaseID); err != nil {
			return "", "", err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,outcome,details)
	VALUES($1,'user',$2,'service_client.create','service_client',$3,'allowed',jsonb_build_object('name',$4::text,'grant_count',$5::int,'expires_at',$6::timestamptz,'scopes',$7::text[]))`, tenantID, actorID, id, name, len(seen), expiresAt, scopes); err != nil {
		return "", "", err
	}
	if err := tx.Commit(); err != nil {
		return "", "", err
	}
	return token, id, nil
}

func RevokeServiceClient(ctx context.Context, db *sql.DB, tenantID, actorID, clientID string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `UPDATE service_clients SET revoked_at=COALESCE(revoked_at,now()) WHERE tenant_id=$1 AND id=$2`, tenantID, clientID)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return sql.ErrNoRows
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO audit_events(tenant_id,actor_type,actor_id,action,resource_type,resource_id,outcome,details)
	VALUES($1,'user',$2,'service_client.revoke','service_client',$3,'allowed','{}'::jsonb)`, tenantID, actorID, clientID); err != nil {
		return err
	}
	return tx.Commit()
}

func hasScope(identity ServiceIdentity, scope string) bool {
	for _, candidate := range identity.Scopes {
		if candidate == scope {
			return true
		}
	}
	return false
}

func serviceRecordValid(identity ServiceIdentity, audience string, revoked bool, now time.Time) bool {
	return audience != "" && subtle.ConstantTimeCompare([]byte(identity.Audience), []byte(audience)) == 1 && !revoked && identity.ExpiresAt.After(now) && hasScope(identity, ScopeRetrievalQuery) && len(identity.Grants) > 0
}

func AllowsGrant(identity ServiceIdentity, collectionID, caseID string) bool {
	for _, grant := range identity.Grants {
		if grant.CollectionID != collectionID {
			continue
		}
		if caseID == "" && grant.CaseID == "" || caseID != "" && grant.CaseID == caseID {
			return true
		}
	}
	return false
}

func isUUID(value string) bool {
	if len(value) != 36 || value[8] != '-' || value[13] != '-' || value[18] != '-' || value[23] != '-' {
		return false
	}
	for i, char := range value {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f' || char >= 'A' && char <= 'F') {
			return false
		}
	}
	return true
}
