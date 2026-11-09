package domain

import "time"

type Tenant struct {
	ID        string
	Name      string
	CreatedAt time.Time
}

type Collection struct {
	ID             string
	TenantID       string
	Name           string
	Description    string
	Classification string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Document struct {
	ID              string
	TenantID        string
	CollectionID    string
	Title           string
	SourceType      string
	LifecycleStatus string
	CreatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type DocumentVersion struct {
	ID                string
	TenantID          string
	DocumentID        string
	VersionNumber     int
	ChecksumSHA256    string
	OriginalObjectKey string
	MediaType         string
	ParserStatus      string
	IndexStatus       string
	ReviewStatus      string
	CreatedAt         time.Time
}

type KnowledgeSource struct {
	ID           string
	TenantID     string
	CollectionID string
	DocumentID   string
	SourceURI    string
	SourceType   string
	TrustStatus  string
	CreatedAt    time.Time
}
