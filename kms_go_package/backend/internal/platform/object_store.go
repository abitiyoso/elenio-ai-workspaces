package platform

import "context"

// ObjectStore is the boundary for S3-compatible original and derived artifacts.
// The concrete credentials and bucket policy are supplied by the deployment environment.
type ObjectStore interface {
	Ping(context.Context) error
	Put(context.Context, string, string, []byte) error
	Get(context.Context, string) ([]byte, error)
	Delete(context.Context, string) error
}
