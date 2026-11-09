// Package simpack embeds the SIM B2B domain pack (the six workflow specialists
// plus the pre-case Case Intake Assistant): definitions, skills, strict output
// schemas, tool grants and baseline evaluation cases.
// Applying it only fills unconfigured drafts; nothing is published.
package simpack

import (
	_ "embed"
	"encoding/json"
)

//go:embed sim_pack_v1.json
var raw []byte

type Pack struct {
	PackKey     string   `json:"pack_key"`
	PackVersion string   `json:"pack_version"`
	Status      string   `json:"status"`
	Note        string   `json:"note"`
	CommonRules []string `json:"common_rules"`
	Agents      []Agent  `json:"agents"`
}

type Agent struct {
	Key              string          `json:"key"`
	ReviewGate       string          `json:"review_gate"`
	SMEReviewerRoles []string        `json:"sme_reviewer_roles"`
	Tools            []string        `json:"tools"`
	Manifest         json.RawMessage `json:"manifest"`
	InputSchema      json.RawMessage `json:"input_schema"`
	OutputSchema     json.RawMessage `json:"output_schema"`
	Skill            Skill           `json:"skill"`
	EvalSuite        Suite           `json:"eval_suite"`
}

type Skill struct {
	Key              string          `json:"key"`
	Semver           string          `json:"semver"`
	Instructions     string          `json:"instructions"`
	Procedures       json.RawMessage `json:"procedures"`
	Examples         json.RawMessage `json:"examples"`
	ForbiddenActions json.RawMessage `json:"forbidden_actions"`
}

type Suite struct {
	Key         string `json:"key"`
	Description string `json:"description"`
	Cases       []Case `json:"cases"`
}

type Case struct {
	Key             string          `json:"key"`
	Classification  string          `json:"classification"`
	Critical        bool            `json:"critical"`
	Input           json.RawMessage `json:"input"`
	EvidenceFixture json.RawMessage `json:"evidence_fixture"`
	Expectations    json.RawMessage `json:"expectations"`
}

// Load returns the embedded pack.
func Load() (Pack, error) {
	var pack Pack
	err := json.Unmarshal(raw, &pack)
	return pack, err
}
