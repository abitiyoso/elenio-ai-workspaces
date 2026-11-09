// Package contracts embeds the JSON Schemas the BFF validates against at runtime.
package contracts

import _ "embed"

// WorkspaceDefinitionSchema is contracts/workspace_definition.schema.json (domain pack manifests).
//
//go:embed workspace_definition.schema.json
var WorkspaceDefinitionSchema []byte
