// Package packs is the domain pack (workspace definition) registry: manifests are loaded from files,
// validated against contracts/workspace_definition.schema.json plus a semantic validator, stored
// immutably with a checksum and served read-only. A manifest is presentation configuration; it never
// grants permissions and never carries code, URLs or HTML.
package packs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

type Brand struct {
	Icon   string `json:"icon,omitempty"`
	Accent string `json:"accent,omitempty"`
}

type Bindings struct {
	FlowcaseWorkflowKey string   `json:"flowcase_workflow_key"`
	ArtifactProvider    string   `json:"artifact_provider"`
	AgentKeys           []string `json:"agent_keys,omitempty"`
	KMSScopePolicyRef   string   `json:"kms_scope_policy_ref,omitempty"`
}

type NavItem struct {
	Key                string `json:"key"`
	Label              string `json:"label"`
	View               string `json:"view"`
	ArtifactType       string `json:"artifact_type,omitempty"`
	ExtensionRef       string `json:"extension_ref,omitempty"`
	RequiredPermission string `json:"required_permission,omitempty"`
	Icon               string `json:"icon,omitempty"`
}

type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Widget   string   `json:"widget"`
	Required bool     `json:"required,omitempty"`
	ReadOnly bool     `json:"read_only,omitempty"`
	Options  []string `json:"options,omitempty"`
	Hint     string   `json:"hint,omitempty"`
	Min      *float64 `json:"min,omitempty"`
	Max      *float64 `json:"max,omitempty"`
}

type Source struct {
	Provider             string `json:"provider"`
	FlowcaseArtifactType string `json:"flowcase_artifact_type"`
	RowsPath             string `json:"rows_path,omitempty"`
	ProducerNodeID       string `json:"producer_node_id,omitempty"`
}

type ArtifactType struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Fields      []Field `json:"fields"`
	Source      *Source `json:"source,omitempty"`
}

type Action struct {
	Key                    string `json:"key"`
	Label                  string `json:"label"`
	Executor               string `json:"executor"`
	AgentKey               string `json:"agent_key,omitempty"`
	CommandKey             string `json:"command_key,omitempty"`
	RegisteredExtensionRef string `json:"registered_extension_ref,omitempty"`
	RequiredPermission     string `json:"required_permission"`
	Placement              string `json:"placement,omitempty"`
	OutputArtifactType     string `json:"output_artifact_type,omitempty"`
}

type IntakeField struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Widget    string `json:"widget"` // text | textarea | date | list
	Required  bool   `json:"required,omitempty"`
	MapsTo    string `json:"maps_to"` // title | variables.<name>
	MaxLength int    `json:"max_length,omitempty"`
	Help      string `json:"help,omitempty"`
}

// IntakeDocuments enables uploading documents into the intake chat: their extracted text fills TargetField
// and Prompt (with {filename}) is sent as the user's message so the agent reads it.
type IntakeDocuments struct {
	TargetField string `json:"target_field"`
	MaxFiles    int    `json:"max_files"`
	Prompt      string `json:"prompt"`
}

type Intake struct {
	AgentKey  string           `json:"agent_key"`
	Greeting  string           `json:"greeting"`
	Starters  []string         `json:"starters,omitempty"`
	Fields    []IntakeField    `json:"fields"`
	Documents *IntakeDocuments `json:"documents,omitempty"`
}

type Manifest struct {
	SchemaVersion string         `json:"schema_version"`
	PackKey       string         `json:"pack_key"`
	Version       string         `json:"version"`
	Title         string         `json:"title"`
	Description   string         `json:"description"`
	Status        string         `json:"status"`
	Brand         *Brand         `json:"brand,omitempty"`
	Bindings      Bindings       `json:"bindings"`
	Navigation    []NavItem      `json:"navigation"`
	ArtifactTypes []ArtifactType `json:"artifact_types"`
	Actions       []Action       `json:"actions"`
	Intake        *Intake        `json:"intake,omitempty"`
}

// ArtifactTypeFor returns the pack artifact type whose source is the given Flowcase artifact type.
func (m *Manifest) ArtifactTypeFor(flowcaseType string) *ArtifactType {
	for i := range m.ArtifactTypes {
		if s := m.ArtifactTypes[i].Source; s != nil && s.FlowcaseArtifactType == flowcaseType {
			return &m.ArtifactTypes[i]
		}
	}
	return nil
}

// Canonical re-encodes JSON with sorted object keys and no insignificant whitespace, so reformatting a
// file does not change its checksum.
func Canonical(raw []byte) ([]byte, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}

// Checksum is "sha256:<hex>" of the canonical manifest JSON.
func Checksum(canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:])
}
