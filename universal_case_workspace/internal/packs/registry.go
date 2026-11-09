package packs

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Pack is one pack version as the registry knows it. Invalid packs are listed with their problems but are
// never pinned to a case or started.
type Pack struct {
	Key            string
	Version        string
	Manifest       *Manifest
	Canonical      json.RawMessage
	Checksum       string
	SourceFile     string
	SchemaErrors   []Problem
	SemanticErrors []Problem
	OtherErrors    []Problem // checksum drift, duplicate version, unreadable file
	Stored         bool
}

func (p *Pack) SchemaOK() bool { return len(p.SchemaErrors) == 0 && p.Manifest != nil }
func (p *Pack) SemanticOK() bool {
	return len(p.SemanticErrors) == 0 && len(p.OtherErrors) == 0 && p.Manifest != nil
}
func (p *Pack) Valid() bool { return p.SchemaOK() && p.SemanticOK() }

// Published: valid and marked published (only such versions are pinned to new cases or started).
func (p *Pack) Published() bool { return p.Valid() && p.Manifest.Status == "published" }

func (p *Pack) Errors() []Problem {
	out := append([]Problem{}, p.SchemaErrors...)
	out = append(out, p.SemanticErrors...)
	return append(out, p.OtherErrors...)
}

// StoredVersion is an immutable registry row.
type StoredVersion struct {
	PackKey    string
	Version    string
	Checksum   string
	Manifest   json.RawMessage // canonical JSON
	Status     string
	SourceFile string
}

// Store persists pack versions immutably (insert-only).
type Store interface {
	ListPackVersions(ctx context.Context) ([]StoredVersion, error)
	InsertPackVersion(ctx context.Context, v StoredVersion) error
}

// Auditor records registry events (pack.loaded, pack.invalid).
type Auditor func(ctx context.Context, action, resource string, metadata map[string]any)

type Registry struct {
	packs []*Pack
	index map[string]*Pack
}

func id(key, version string) string { return key + "@" + version }

// semver parses x.y.z; invalid versions sort first.
func semver(v string) [3]int {
	var out [3]int
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return [3]int{-1, -1, -1}
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return [3]int{-1, -1, -1}
		}
		out[i] = n
	}
	return out
}

func newer(a, b string) bool {
	x, y := semver(a), semver(b)
	for i := range 3 {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

// FromRaw validates one manifest document.
func FromRaw(raw []byte, sourceFile string) *Pack {
	p := &Pack{SourceFile: sourceFile}
	canon, err := Canonical(raw)
	if err != nil {
		p.Key = strings.TrimSuffix(filepath.Base(sourceFile), ".json")
		p.OtherErrors = []Problem{{Code: "invalid_json", Message: "manifest is not valid JSON"}}
		return p
	}
	p.Canonical, p.Checksum = canon, Checksum(canon)
	m, schemaProblems, semanticProblems := Validate(canon)
	p.Manifest, p.SchemaErrors, p.SemanticErrors = m, schemaProblems, semanticProblems
	if m != nil {
		p.Key, p.Version = m.PackKey, m.Version
	}
	if p.Key == "" {
		p.Key = strings.TrimSuffix(filepath.Base(sourceFile), ".json")
	}
	return p
}

// Load reads every *.json manifest in dir, validates it and reconciles it with the stored versions:
// a new valid version is stored; a stored version whose checksum differs from the file is never
// overwritten and that pack version becomes invalid (fail closed for that pack only). Stored versions
// without a file stay available (cases pinned to them keep rendering).
func Load(ctx context.Context, dir string, store Store, audit Auditor, log *slog.Logger) (*Registry, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	if audit == nil {
		audit = func(context.Context, string, string, map[string]any) {}
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	stored := map[string]StoredVersion{}
	if store != nil {
		rows, err := store.ListPackVersions(ctx)
		if err != nil {
			return nil, fmt.Errorf("load stored pack versions: %w", err)
		}
		for _, r := range rows {
			stored[id(r.PackKey, r.Version)] = r
		}
	}
	reg := &Registry{index: map[string]*Pack{}}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		var p *Pack
		if err != nil {
			p = &Pack{Key: filepath.Base(f), SourceFile: filepath.Base(f), OtherErrors: []Problem{{Code: "unreadable", Message: "manifest file cannot be read"}}}
		} else {
			p = FromRaw(raw, filepath.Base(f))
		}
		k := id(p.Key, p.Version)
		if prev, dup := reg.index[k]; dup && p.Version != "" {
			p.OtherErrors = append(p.OtherErrors, Problem{Path: "/version", Code: "duplicate_version",
				Message: fmt.Sprintf("version %s of %s is also defined in %s", p.Version, p.Key, prev.SourceFile)})
			k = id(p.Key, p.Version+"#"+p.SourceFile)
		}
		if s, ok := stored[id(p.Key, p.Version)]; ok && p.Version != "" && len(p.OtherErrors) == 0 {
			delete(stored, id(p.Key, p.Version))
			if s.Checksum != p.Checksum {
				p.OtherErrors = append(p.OtherErrors, Problem{Code: "checksum_drift",
					Message: fmt.Sprintf("file content differs from the stored immutable version (%s); publish a new version instead", s.Checksum)})
				log.ErrorContext(ctx, "pack version checksum drift; pack disabled", "pack_key", p.Key, "version", p.Version, "stored", s.Checksum, "file", p.Checksum)
			} else {
				p.Stored = true
			}
		}
		if p.Valid() && !p.Stored && store != nil {
			if err := store.InsertPackVersion(ctx, StoredVersion{PackKey: p.Key, Version: p.Version, Checksum: p.Checksum, Manifest: p.Canonical,
				Status: p.Manifest.Status, SourceFile: p.SourceFile}); err != nil {
				return nil, fmt.Errorf("store pack %s %s: %w", p.Key, p.Version, err)
			}
			p.Stored = true
		}
		reg.add(k, p)
		meta := map[string]any{"version": p.Version, "checksum": p.Checksum, "file": p.SourceFile}
		if p.Valid() {
			audit(ctx, "pack.loaded", "pack:"+p.Key, meta)
			log.InfoContext(ctx, "pack loaded", "pack_key", p.Key, "version", p.Version, "checksum", p.Checksum)
		} else {
			codes := []string{}
			for _, e := range p.Errors() {
				codes = append(codes, e.Code)
			}
			meta["errors"] = codes
			audit(ctx, "pack.invalid", "pack:"+p.Key, meta)
			log.WarnContext(ctx, "pack invalid", "pack_key", p.Key, "version", p.Version, "file", p.SourceFile, "errors", strings.Join(codes, ","))
		}
	}
	for _, s := range stored { // stored versions whose file is gone
		p := FromRaw(s.Manifest, s.SourceFile)
		p.Stored = true
		if p.Checksum != s.Checksum {
			p.OtherErrors = append(p.OtherErrors, Problem{Code: "checksum_drift", Message: "stored manifest does not match its checksum"})
		}
		reg.add(id(p.Key, p.Version), p)
	}
	sort.SliceStable(reg.packs, func(i, j int) bool {
		a, b := reg.packs[i], reg.packs[j]
		if a.Key != b.Key {
			return a.Key < b.Key
		}
		return newer(a.Version, b.Version)
	})
	return reg, nil
}

// NewStatic builds a registry from already validated packs (tests).
func NewStatic(ps ...*Pack) *Registry {
	r := &Registry{index: map[string]*Pack{}}
	for _, p := range ps {
		r.add(id(p.Key, p.Version), p)
	}
	return r
}

func (r *Registry) add(k string, p *Pack) {
	r.packs = append(r.packs, p)
	r.index[k] = p
}

// All lists every pack version (valid or not), by key then newest version first.
func (r *Registry) All() []*Pack { return r.packs }

// Get returns one version, or nil.
func (r *Registry) Get(key, version string) *Pack { return r.index[id(key, version)] }

// LatestPublished is the newest valid published version of key, or nil.
func (r *Registry) LatestPublished(key string) *Pack {
	var best *Pack
	for _, p := range r.packs {
		if p.Key == key && p.Published() && (best == nil || newer(p.Version, best.Version)) {
			best = p
		}
	}
	return best
}

// LatestForWorkflow is the newest valid published pack bound to a Flowcase workflow key, or nil.
func (r *Registry) LatestForWorkflow(workflowKey string) *Pack {
	var best *Pack
	for _, p := range r.packs {
		if p.Published() && p.Manifest.Bindings.FlowcaseWorkflowKey == workflowKey && (best == nil || newer(p.Version, best.Version)) {
			best = p
		}
	}
	return best
}
