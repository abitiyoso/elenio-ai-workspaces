// Package packs embeds the domain packs (SIM tender, internal PoC): their catalog registrations and
// workflow templates. Packs are configuration; the engine has no pack-specific code paths.
package packs

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"

	"github.com/elenio/flowcase/internal/workflows/catalog"
)

//go:embed sim/*.json poc/*.json
var files embed.FS

var packDirs = []string{"sim", "poc"}

type Pack struct {
	Key         string           `json:"key"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Templates   []string         `json:"templates"`
	Evolutions  []string         `json:"evolutions"`
	Catalog     catalog.Snapshot `json:"catalog"`
	dir         string
}

// Template is a definition document a designer can start a new workflow from.
type Template struct {
	Pack        string          `json:"pack"`
	Key         string          `json:"key"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Document    json.RawMessage `json:"-"`
}

func Load() ([]Pack, error) {
	var out []Pack
	for _, dir := range packDirs {
		raw, err := files.ReadFile(path.Join(dir, "pack.json"))
		if err != nil {
			return nil, err
		}
		var p Pack
		if err := json.Unmarshal(raw, &p); err != nil {
			return nil, fmt.Errorf("pack %s: %w", dir, err)
		}
		p.dir = dir
		for i := range p.Catalog.Policies {
			p.Catalog.Policies[i].Pack = p.Key
		}
		for i := range p.Catalog.Agents {
			p.Catalog.Agents[i].Pack = p.Key
		}
		for i := range p.Catalog.Connectors {
			p.Catalog.Connectors[i].Pack = p.Key
		}
		out = append(out, p)
	}
	return out, nil
}

// Catalog merges every pack's registrations.
func Catalog(ps []Pack) (*catalog.Static, error) {
	c := catalog.NewStatic()
	for _, p := range ps {
		if err := c.Merge(p.Catalog); err != nil {
			return nil, fmt.Errorf("pack %s: %w", p.Key, err)
		}
	}
	return c, nil
}

func (p Pack) document(name string) (json.RawMessage, error) {
	return files.ReadFile(path.Join(p.dir, name))
}

func Templates(ps []Pack) ([]Template, error) {
	var out []Template
	for _, p := range ps {
		for _, name := range p.Templates {
			raw, err := p.document(name)
			if err != nil {
				return nil, err
			}
			var head struct{ Key, Name, Description string }
			if err := json.Unmarshal(raw, &head); err != nil {
				return nil, fmt.Errorf("template %s/%s: %w", p.Key, name, err)
			}
			out = append(out, Template{Pack: p.Key, Key: head.Key, Name: head.Name, Description: head.Description, Document: raw})
		}
	}
	return out, nil
}

// Evolutions are follow-up draft versions shipped as examples (e.g. SIM v2 draft).
func Evolutions(ps []Pack) ([]json.RawMessage, error) {
	var out []json.RawMessage
	for _, p := range ps {
		for _, name := range p.Evolutions {
			raw, err := p.document(name)
			if err != nil {
				return nil, err
			}
			out = append(out, raw)
		}
	}
	return out, nil
}
