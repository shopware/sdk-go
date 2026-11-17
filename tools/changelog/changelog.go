// Package main implements the repository's changelog + release-notes tool.
//
// It reads small per-change JSON fragments under .changelog/ and produces the
// GitHub Release body in the documented "Module Highlights" format, computes
// the next semver per released module, validates fragments, and scaffolds new
// ones. See docs/CHANGELOG_AND_RELEASES.md.
//
// This file holds the pure, git-free logic so it can be unit-tested directly.
package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Module is a released module in this repository. The registry below is the
// single source of truth for which modules the tooling knows about — keep it in
// sync with the module checklist in docs/RELEASING.md ("Adding a new module")
// when adding a new module.
type Module struct {
	// Dir is the repo-relative module directory, used as the "modules" value in
	// fragments: "." for the core module, "integrations/gin" for the gin one,
	// "clients/adminapi" for the Admin API client.
	Dir string
	// ImportPath is the Go import path shown in the release notes.
	ImportPath string
	// TagPrefix is prepended to the semver to form the git tag and the version
	// string shown in the notes: "" for core (tag v1.2.3), "integrations/gin/"
	// for gin (tag integrations/gin/v1.2.3), "clients/adminapi/" for the client.
	TagPrefix string
	// DependsOn lists the repo-relative dirs of other modules this one requires
	// (via its go.mod). Used by the release-order guard: when releasing this
	// module, each dependency's required version must already be tagged or be
	// released in the same run. The client has none; core depends on the
	// client; gin depends on core and the client.
	DependsOn []string
}

// Modules is the ordered registry. Order defines the order modules appear in
// the release notes and the order they are tagged, and must follow the
// dependency order (clients/adminapi, then core, then integrations): a module is
// always listed after everything in its DependsOn — see "Dependency order" in
// RELEASING.md.
var Modules = []Module{
	{Dir: "clients/adminapi", ImportPath: "github.com/shopware/sdk-go/clients/adminapi", TagPrefix: "clients/adminapi/"},
	{Dir: ".", ImportPath: "github.com/shopware/sdk-go", TagPrefix: "", DependsOn: []string{"clients/adminapi"}},
	{Dir: "integrations/gin", ImportPath: "github.com/shopware/sdk-go/integrations/gin", TagPrefix: "integrations/gin/", DependsOn: []string{".", "clients/adminapi"}},
}

func moduleByDir(dir string) (Module, bool) {
	for _, m := range Modules {
		if m.Dir == dir {
			return m, true
		}
	}
	return Module{}, false
}

// changeType is the category of a change; it selects the release-notes label
// and, unless overridden, the default version bump.
type changeType string

const (
	typeFeature       changeType = "feature"
	typeBugfix        changeType = "bugfix"
	typeDocumentation changeType = "documentation"
	typeDependency    changeType = "dependency"
	typeRelease       changeType = "release"
)

// label is the human-readable heading used in the notes for each type.
var label = map[changeType]string{
	typeFeature:       "Feature",
	typeBugfix:        "Bug Fix",
	typeDocumentation: "Documentation",
	typeDependency:    "Dependency",
	typeRelease:       "Release",
}

// moduleBulletOrder is the order change types are listed within a module's
// notes block.
var moduleBulletOrder = []changeType{
	typeFeature, typeBugfix, typeDocumentation, typeDependency, typeRelease,
}

// defaultBump maps a change type to the version bump it implies when a fragment
// does not set "bump" explicitly. Breaking changes must set "bump":"major".
var defaultBump = map[changeType]bump{
	typeFeature:       bumpMinor,
	typeBugfix:        bumpPatch,
	typeDocumentation: bumpPatch,
	typeDependency:    bumpPatch,
	typeRelease:       bumpMinor,
}

// bump is a semver increment level.
type bump int

const (
	bumpNone bump = iota
	bumpPatch
	bumpMinor
	bumpMajor
)

func parseBump(s string) (bump, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "patch":
		return bumpPatch, nil
	case "minor":
		return bumpMinor, nil
	case "major":
		return bumpMajor, nil
	default:
		return bumpNone, fmt.Errorf("invalid bump %q (want patch|minor|major)", s)
	}
}

func (b bump) String() string {
	switch b {
	case bumpPatch:
		return "patch"
	case bumpMinor:
		return "minor"
	case bumpMajor:
		return "major"
	default:
		return "none"
	}
}

// Entry is a single changelog fragment (.changelog/<id>.json).
type Entry struct {
	Type        string   `json:"type"`
	Bump        string   `json:"bump,omitempty"`
	Description string   `json:"description"`
	Modules     []string `json:"modules"`
}

// Validate reports every problem with an entry (empty slice means valid).
func (e Entry) Validate() []string {
	var issues []string
	if _, ok := label[changeType(e.Type)]; !ok {
		issues = append(issues, fmt.Sprintf("invalid type %q", e.Type))
	}
	if e.Bump != "" {
		if _, err := parseBump(e.Bump); err != nil {
			issues = append(issues, err.Error())
		}
	}
	if strings.TrimSpace(e.Description) == "" {
		issues = append(issues, "description is required")
	}
	if len(e.Modules) == 0 {
		issues = append(issues, "at least one module is required")
	}
	for _, d := range e.Modules {
		if _, ok := moduleByDir(d); !ok {
			issues = append(issues, fmt.Sprintf("unknown module %q", d))
		}
	}
	return issues
}

// effectiveBump returns the bump an entry contributes: its explicit "bump" if
// set, otherwise the default for its type.
func (e Entry) effectiveBump() bump {
	if e.Bump != "" {
		b, err := parseBump(e.Bump)
		if err == nil {
			return b
		}
	}
	return defaultBump[changeType(e.Type)]
}

// SemVer is a parsed vMAJOR.MINOR.PATCH version.
type SemVer struct{ Major, Minor, Patch int }

// ParseSemVer parses "v1.2.3" (leading "v" required).
func ParseSemVer(s string) (SemVer, error) {
	raw := strings.TrimPrefix(s, "v")
	parts := strings.Split(raw, ".")
	if len(parts) != 3 || !strings.HasPrefix(s, "v") {
		return SemVer{}, fmt.Errorf("invalid semver %q", s)
	}
	var v SemVer
	var err error
	if v.Major, err = strconv.Atoi(parts[0]); err != nil {
		return SemVer{}, fmt.Errorf("invalid semver %q: %w", s, err)
	}
	if v.Minor, err = strconv.Atoi(parts[1]); err != nil {
		return SemVer{}, fmt.Errorf("invalid semver %q: %w", s, err)
	}
	if v.Patch, err = strconv.Atoi(parts[2]); err != nil {
		return SemVer{}, fmt.Errorf("invalid semver %q: %w", s, err)
	}
	return v, nil
}

func (v SemVer) String() string { return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less orders versions ascending.
func (v SemVer) Less(o SemVer) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}

// apply returns the version after applying a bump.
func (v SemVer) apply(b bump) SemVer {
	switch b {
	case bumpMajor:
		return SemVer{v.Major + 1, 0, 0}
	case bumpMinor:
		return SemVer{v.Major, v.Minor + 1, 0}
	case bumpPatch:
		return SemVer{v.Major, v.Minor, v.Patch + 1}
	default:
		return v
	}
}

// MaxSemVer returns the highest version in the list, ok=false if empty.
func MaxSemVer(vs []SemVer) (SemVer, bool) {
	if len(vs) == 0 {
		return SemVer{}, false
	}
	max := vs[0]
	for _, v := range vs[1:] {
		if max.Less(v) {
			max = v
		}
	}
	return max, true
}

// Release is one module's computed release: its new version and the bump used.
type Release struct {
	Module     Module
	Current    SemVer
	Next       SemVer
	Bump       bump
	EntryCount int
}

// ComputeReleases determines, from the pending entries and each module's
// current version, which modules must be released and at what version. Modules
// with no entries are omitted. It returns an error if any contributing entry
// requests a major bump (majors require manual /vN module path migration and
// are not automated).
func ComputeReleases(entries []Entry, current map[string]SemVer) ([]Release, error) {
	maxBump := map[string]bump{}
	count := map[string]int{}
	for _, e := range entries {
		b := e.effectiveBump()
		for _, d := range e.Modules {
			count[d]++
			if b > maxBump[d] {
				maxBump[d] = b
			}
		}
	}

	var releases []Release
	for _, m := range Modules { // registry order = tag/notes order
		b, ok := maxBump[m.Dir]
		if !ok || b == bumpNone {
			continue
		}
		if b == bumpMajor {
			return nil, fmt.Errorf("module %q requests a major bump; major releases require a manual /vN module-path migration (see docs/RELEASING.md) and are not automated", m.Dir)
		}
		cur := current[m.Dir] // zero value v0.0.0 if module was never tagged
		releases = append(releases, Release{
			Module:     m,
			Current:    cur,
			Next:       cur.apply(b),
			Bump:       b,
			EntryCount: count[m.Dir],
		})
	}
	return releases, nil
}

// RequiredVersionFunc returns the version of dependency importPath required by
// the module in dir (from its go.mod), and whether such a require exists.
type RequiredVersionFunc func(dir, importPath string) (SemVer, bool, error)

// CheckDependencies enforces the release-order guard: for every module being
// released that depends on another module, the dependency version pinned in
// its go.mod must either already be tagged (<= the dependency's latest tag) or
// be the exact version the dependency is being released at in this same run.
// Because modules are tagged in registry order (dependencies first), a
// dependency released in the same run is already tagged by the time the
// dependent is.
func CheckDependencies(releases []Release, current map[string]SemVer, required RequiredVersionFunc) error {
	releasing := map[string]SemVer{}
	for _, r := range releases {
		releasing[r.Module.Dir] = r.Next
	}
	for _, r := range releases {
		for _, depDir := range r.Module.DependsOn {
			dep, ok := moduleByDir(depDir)
			if !ok {
				return fmt.Errorf("module %q declares unknown dependency %q", r.Module.Dir, depDir)
			}
			reqVer, found, err := required(r.Module.Dir, dep.ImportPath)
			if err != nil {
				return err
			}
			if !found {
				continue // dependent does not require this module — nothing to guard
			}
			// (a) already tagged: required <= dependency's latest existing tag.
			if cur, ok := current[depDir]; ok && !cur.Less(reqVer) {
				continue
			}
			// (b) being released now at exactly the required version.
			if next, ok := releasing[depDir]; ok && next == reqVer {
				continue
			}
			return fmt.Errorf(
				"module %q requires %s %s, which is neither already tagged nor being released in this run; "+
					"release %s first (or bump %s/go.mod once %s is tagged)",
				r.Module.Dir, dep.ImportPath, reqVer, dep.ImportPath, r.Module.Dir, dep.ImportPath)
		}
	}
	return nil
}

// notesVersion renders the version as it appears in the release notes and git
// tag: bare for core (v1.2.3), prefixed otherwise (integrations/gin/v1.2.3).
func (r Release) notesVersion() string { return r.Module.TagPrefix + r.Next.String() }

// Tag is the git tag for this release.
func (r Release) Tag() string { return r.Module.TagPrefix + r.Next.String() }

// RenderNotes produces the GitHub Release body markdown. general is optional
// maintainer-supplied free-text; when non-empty it forms the "General
// Highlights" section (verbatim markdown). The section is omitted entirely when
// no general text is provided. "Module Highlights" is always present.
func RenderNotes(entries []Entry, releases []Release, general string) string {
	var b strings.Builder

	// General Highlights: only when the maintainer supplies text.
	if general = strings.TrimRight(general, "\n"); general != "" {
		b.WriteString("## General Highlights\n\n")
		b.WriteString(general)
		b.WriteString("\n\n")
	}

	// Module Highlights: one block per released module, registry order.
	b.WriteString("## Module Highlights\n")
	byModule := map[string][]Entry{}
	for _, e := range entries {
		for _, d := range e.Modules {
			byModule[d] = append(byModule[d], e)
		}
	}
	for _, r := range releases {
		fmt.Fprintf(&b, "* `%s`: %s\n", r.Module.ImportPath, r.notesVersion())
		for _, ct := range moduleBulletOrder {
			var descs []string
			for _, e := range byModule[r.Module.Dir] {
				if changeType(e.Type) == ct {
					descs = append(descs, e.Description)
				}
			}
			sort.Strings(descs)
			for _, d := range descs {
				fmt.Fprintf(&b, "  * **%s**: %s\n", label[ct], d)
			}
		}
	}
	return b.String()
}
