// Package knowledge is the KnowledgeProvider port (KMS sources, versions, anchors with ACL). The MVP does
// not call KMS: source titles shown in the workspace come from Agent Studio's result envelope
// ("sources") as stored in Flowcase artifact content and in intake turns. A real adapter must call KMS as
// the user (or with a delegated service client) and must never return metadata of sources the user
// cannot read.
package knowledge

import (
	"context"
	"errors"
)

// ErrNotImplemented is returned by the MVP stub; callers surface it as a kms partial error.
var ErrNotImplemented = errors.New("knowledge provider not implemented in the MVP")

type SourceRef struct {
	SourceID  string `json:"source_id"`
	VersionID string `json:"version_id,omitempty"`
	Anchor    string `json:"anchor,omitempty"`
}

type Source struct {
	SourceRef
	Title  string `json:"title"`
	Status string `json:"status"` // published | withdrawn | expired, as KMS reports it
}

// Provider is the port. UserID is the Control Plane user id the lookup is performed for.
type Provider interface {
	GetSource(ctx context.Context, userID string, ref SourceRef) (Source, error)
}

// Stub implements Provider without KMS.
type Stub struct{}

func (Stub) GetSource(context.Context, string, SourceRef) (Source, error) {
	return Source{}, ErrNotImplemented
}
