package config

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Supported AUTH_MODE values. AuthModeControlPlaneJWT verifies the per-request ES256/RS256 bearer token that
// the AI Workspace Control Plane BFF injects when it reverse-proxies /apps/kms/*.
const (
	AuthModeMock            = "mock"
	AuthModeTrusted         = "trusted"
	AuthModeControlPlaneJWT = "control_plane_jwt"
)

type Config struct {
	Environment            string
	HTTPAddress            string
	LogLevel               slog.Level
	AuthMode               string
	MockUserID             string
	MockTenantID           string
	ControlPlaneAuth       ControlPlaneAuthConfig
	PostgresURL            string
	ElasticsearchURL       string
	ElasticsearchIndex     string
	EmbeddingEndpoint      string
	EmbeddingAPIKey        string
	EmbeddingModel         string
	EmbeddingDimensions    int
	ObjectStorageEndpoint  string
	ObjectStorageAccessKey string
	ObjectStorageSecretKey string
	ObjectStorageBucket    string
	ObjectStorageRegion    string
	DoclingBaseURL         string
	DoclingAPIKey          string
	MigrationsDir          string
	MaxUploadBytes         int64
	MetricsToken           string
	// SelfApprovalExempt lists principal ids (Control Plane user ids) that may review their own submissions.
	// Development only: it exists for single-account walkthroughs and is refused in any other environment.
	SelfApprovalExempt []string
}

// ControlPlaneAuthConfig is populated only when AUTH_MODE=control_plane_jwt.
type ControlPlaneAuthConfig struct {
	Issuer       string
	Audience     string
	JWKSURL      string
	TenantMap    map[string]string // Control Plane tenant key -> KMS tenant UUID (lower-case)
	MaxTokenTTL  time.Duration
	ClockSkew    time.Duration
	JWKSCacheTTL time.Duration
}

func Load() (Config, error) {
	c := Config{
		Environment:            get("APP_ENV", "production"),
		HTTPAddress:            get("HTTP_ADDRESS", ":8080"),
		AuthMode:               get("AUTH_MODE", "mock"),
		MockUserID:             get("DEV_AUTH_USER_ID", "00000000-0000-4000-8000-000000000001"),
		MockTenantID:           get("DEV_AUTH_TENANT_ID", "00000000-0000-4000-8000-000000000002"),
		PostgresURL:            get("DATABASE_URL", "postgres://kms:kms@localhost:5432/kms?sslmode=disable"),
		ElasticsearchURL:       get("ELASTICSEARCH_URL", "http://localhost:9200"),
		ElasticsearchIndex:     get("ELASTICSEARCH_INDEX", "kms_chunks_v1"),
		EmbeddingEndpoint:      strings.TrimRight(os.Getenv("EMBEDDING_ENDPOINT"), "/"),
		EmbeddingAPIKey:        os.Getenv("EMBEDDING_API_KEY"),
		EmbeddingModel:         os.Getenv("EMBEDDING_MODEL_ID"),
		ObjectStorageEndpoint:  strings.TrimRight(os.Getenv("OBJECT_STORAGE_ENDPOINT"), "/"),
		ObjectStorageAccessKey: os.Getenv("OBJECT_STORAGE_ACCESS_KEY"),
		ObjectStorageSecretKey: os.Getenv("OBJECT_STORAGE_SECRET_KEY"),
		ObjectStorageBucket:    os.Getenv("OBJECT_STORAGE_BUCKET"),
		ObjectStorageRegion:    get("OBJECT_STORAGE_REGION", "us-east-1"),
		DoclingBaseURL:         strings.TrimRight(os.Getenv("DOCLING_BASE_URL"), "/"),
		DoclingAPIKey:          os.Getenv("DOCLING_API_KEY"),
		MigrationsDir:          get("MIGRATIONS_DIR", "./migrations"),
		MetricsToken:           os.Getenv("METRICS_TOKEN"),
	}
	var err error
	c.EmbeddingDimensions, err = strconv.Atoi(get("EMBEDDING_DIMENSIONS", "0"))
	if err != nil || c.EmbeddingDimensions < 0 || c.EmbeddingDimensions > 4096 {
		return Config{}, fmt.Errorf("EMBEDDING_DIMENSIONS must be an integer between 0 and 4096")
	}
	switch get("LOG_LEVEL", "info") {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return Config{}, fmt.Errorf("LOG_LEVEL must be debug, info, warn, or error")
	}
	if c.Environment != "development" && c.AuthMode == "mock" {
		return Config{}, fmt.Errorf("AUTH_MODE=mock is allowed only when APP_ENV=development")
	}
	if c.AuthMode != AuthModeMock && c.AuthMode != AuthModeTrusted && c.AuthMode != AuthModeControlPlaneJWT {
		return Config{}, fmt.Errorf("AUTH_MODE must be mock, trusted, or control_plane_jwt")
	}
	if c.AuthMode == AuthModeControlPlaneJWT {
		if c.ControlPlaneAuth, err = loadControlPlaneAuth(c.Environment); err != nil {
			return Config{}, err
		}
	}
	if c.AuthMode == "trusted" && c.Environment == "development" {
		// Trusted mode is also valid in development for host integration work.
	}
	if c.ElasticsearchIndex == "" {
		return Config{}, fmt.Errorf("ELASTICSEARCH_INDEX must not be empty")
	}
	if c.EmbeddingDimensions > 0 && (c.EmbeddingEndpoint == "" || c.EmbeddingModel == "") {
		return Config{}, fmt.Errorf("EMBEDDING_ENDPOINT and EMBEDDING_MODEL_ID are required when EMBEDDING_DIMENSIONS is set")
	}
	if (c.EmbeddingEndpoint == "") != (c.EmbeddingModel == "") || (c.EmbeddingEndpoint == "" && c.EmbeddingDimensions != 0) {
		return Config{}, fmt.Errorf("EMBEDDING_ENDPOINT, EMBEDDING_MODEL_ID, and EMBEDDING_DIMENSIONS must be configured together")
	}
	if c.EmbeddingEndpoint != "" && c.EmbeddingDimensions == 0 {
		return Config{}, fmt.Errorf("EMBEDDING_DIMENSIONS is required when EMBEDDING_ENDPOINT is configured")
	}
	if c.EmbeddingEndpoint != "" && !validHTTPURL(c.EmbeddingEndpoint) {
		return Config{}, fmt.Errorf("EMBEDDING_ENDPOINT must be an HTTP(S) URL")
	}
	if c.DoclingBaseURL != "" && !validHTTPURL(c.DoclingBaseURL) {
		return Config{}, fmt.Errorf("DOCLING_BASE_URL must be an HTTP(S) URL")
	}
	if c.MigrationsDir == "" {
		return Config{}, fmt.Errorf("MIGRATIONS_DIR must not be empty")
	}
	if c.ObjectStorageEndpoint != "" && (c.ObjectStorageAccessKey == "" || c.ObjectStorageSecretKey == "" || c.ObjectStorageBucket == "") {
		return Config{}, fmt.Errorf("OBJECT_STORAGE_ACCESS_KEY, OBJECT_STORAGE_SECRET_KEY, and OBJECT_STORAGE_BUCKET are required when OBJECT_STORAGE_ENDPOINT is configured")
	}
	if (c.ObjectStorageAccessKey != "" || c.ObjectStorageSecretKey != "" || c.ObjectStorageBucket != "") && c.ObjectStorageEndpoint == "" {
		return Config{}, fmt.Errorf("OBJECT_STORAGE_ENDPOINT is required when object storage credentials or bucket are configured")
	}
	if c.MetricsToken != "" && len(c.MetricsToken) < 32 {
		return Config{}, fmt.Errorf("METRICS_TOKEN must contain at least 32 characters when set")
	}
	c.MaxUploadBytes, err = strconv.ParseInt(get("MAX_UPLOAD_BYTES", "52428800"), 10, 64)
	if err != nil || c.MaxUploadBytes <= 0 {
		return Config{}, fmt.Errorf("MAX_UPLOAD_BYTES must be a positive integer")
	}
	if c.SelfApprovalExempt, err = parseSelfApprovalExempt(os.Getenv("SELF_APPROVAL_EXEMPT_SUBJECTS"), c.Environment); err != nil {
		return Config{}, err
	}
	return c, nil
}

// parseSelfApprovalExempt reads "uuid,uuid2". Separation of duties stays enforced for everyone else.
func parseSelfApprovalExempt(raw, environment string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, entry := range strings.Split(raw, ",") {
		id := strings.ToLower(strings.TrimSpace(entry))
		if id == "" {
			continue
		}
		if !isUUID(id) {
			return nil, fmt.Errorf("SELF_APPROVAL_EXEMPT_SUBJECTS must list principal UUIDs separated by commas")
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > 0 && environment != "development" {
		return nil, fmt.Errorf("SELF_APPROVAL_EXEMPT_SUBJECTS is allowed only when APP_ENV=development")
	}
	return out, nil
}

func loadControlPlaneAuth(environment string) (ControlPlaneAuthConfig, error) {
	cp := ControlPlaneAuthConfig{
		Issuer:   strings.TrimSpace(os.Getenv("AUTH_ISSUER")),
		Audience: strings.TrimSpace(get("AUTH_AUDIENCE", "kms")),
		JWKSURL:  strings.TrimSpace(os.Getenv("AUTH_JWKS_URL")),
	}
	if cp.Issuer == "" {
		return cp, fmt.Errorf("AUTH_ISSUER is required when AUTH_MODE=control_plane_jwt")
	}
	if u, err := url.Parse(cp.Issuer); err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") || u.RawQuery != "" || u.Fragment != "" {
		return cp, fmt.Errorf("AUTH_ISSUER must be an absolute HTTP(S) origin, exactly as the Control Plane writes it in the iss claim")
	}
	if cp.Audience == "" {
		return cp, fmt.Errorf("AUTH_AUDIENCE must not be empty")
	}
	if cp.JWKSURL == "" {
		return cp, fmt.Errorf("AUTH_JWKS_URL is required when AUTH_MODE=control_plane_jwt")
	}
	jwksURL, err := url.Parse(cp.JWKSURL)
	if err != nil || jwksURL.Host == "" || (jwksURL.Scheme != "http" && jwksURL.Scheme != "https") {
		return cp, fmt.Errorf("AUTH_JWKS_URL must be an absolute HTTP(S) URL")
	}
	if jwksURL.Scheme != "https" && environment != "development" {
		return cp, fmt.Errorf("AUTH_JWKS_URL must use https unless APP_ENV=development")
	}
	if cp.TenantMap, err = parseTenantMap(os.Getenv("AUTH_TENANT_MAP")); err != nil {
		return cp, err
	}
	if cp.MaxTokenTTL, err = durationSetting("AUTH_MAX_TOKEN_TTL", "15m", time.Second, time.Hour); err != nil {
		return cp, err
	}
	if cp.ClockSkew, err = durationSetting("AUTH_CLOCK_SKEW", "60s", 0, 5*time.Minute); err != nil {
		return cp, err
	}
	if cp.JWKSCacheTTL, err = durationSetting("AUTH_JWKS_CACHE_TTL", "10m", 30*time.Second, 24*time.Hour); err != nil {
		return cp, err
	}
	return cp, nil
}

// parseTenantMap reads "key=uuid,key2=uuid2". Keys and UUIDs must each be unique so two Control Plane
// tenants can never be merged into one KMS tenant.
func parseTenantMap(raw string) (map[string]string, error) {
	out := map[string]string{}
	seenTenant := map[string]string{}
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		key, tenantID, ok := strings.Cut(entry, "=")
		key, tenantID = strings.TrimSpace(key), strings.ToLower(strings.TrimSpace(tenantID))
		if !ok || key == "" || strings.ContainsAny(key, " \t=") || !isUUID(tenantID) {
			return nil, fmt.Errorf("AUTH_TENANT_MAP entries must be <tenant-key>=<kms-tenant-uuid>")
		}
		if _, dup := out[key]; dup {
			return nil, fmt.Errorf("AUTH_TENANT_MAP lists tenant key %q more than once", key)
		}
		if other, dup := seenTenant[tenantID]; dup {
			return nil, fmt.Errorf("AUTH_TENANT_MAP maps tenant keys %q and %q to the same KMS tenant", other, key)
		}
		out[key], seenTenant[tenantID] = tenantID, key
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("AUTH_TENANT_MAP is required when AUTH_MODE=control_plane_jwt")
	}
	return out, nil
}

func durationSetting(key, fallback string, min, max time.Duration) (time.Duration, error) {
	value, err := time.ParseDuration(get(key, fallback))
	if err != nil || value < min || value > max {
		return 0, fmt.Errorf("%s must be a duration between %s and %s", key, min, max)
	}
	return value, nil
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for i, char := range value {
		switch i {
		case 8, 13, 18, 23:
			if char != '-' {
				return false
			}
		default:
			if !(char >= '0' && char <= '9' || char >= 'a' && char <= 'f') {
				return false
			}
		}
	}
	return true
}

func validHTTPURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Host != "" && (u.Scheme == "http" || u.Scheme == "https")
}

func get(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok && value != "" {
		return value
	}
	return fallback
}
