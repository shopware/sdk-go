package main

import (
	"strings"
	"testing"
)

func TestParseSemVer(t *testing.T) {
	tests := []struct {
		in      string
		want    SemVer
		wantErr bool
	}{
		{"v1.2.3", SemVer{1, 2, 3}, false},
		{"v0.0.0", SemVer{0, 0, 0}, false},
		{"1.2.3", SemVer{}, true},    // missing leading v
		{"v1.2", SemVer{}, true},     // too few parts
		{"v1.2.x", SemVer{}, true},   // non-numeric
		{"v1.2.3.4", SemVer{}, true}, // too many parts
	}
	for _, tt := range tests {
		got, err := ParseSemVer(tt.in)
		if (err != nil) != tt.wantErr {
			t.Errorf("ParseSemVer(%q) err=%v wantErr=%v", tt.in, err, tt.wantErr)
			continue
		}
		if err == nil && got != tt.want {
			t.Errorf("ParseSemVer(%q)=%v want %v", tt.in, got, tt.want)
		}
	}
}

func TestSemVerApply(t *testing.T) {
	base := SemVer{1, 2, 3}
	cases := map[bump]SemVer{
		bumpPatch: {1, 2, 4},
		bumpMinor: {1, 3, 0},
		bumpMajor: {2, 0, 0},
	}
	for b, want := range cases {
		if got := base.apply(b); got != want {
			t.Errorf("apply(%s)=%v want %v", b, got, want)
		}
	}
}

func TestEntryValidate(t *testing.T) {
	tests := []struct {
		name  string
		entry Entry
		valid bool
	}{
		{"good", Entry{Type: "feature", Description: "x", Modules: []string{"."}}, true},
		{"good with bump", Entry{Type: "bugfix", Bump: "minor", Description: "x", Modules: []string{"integrations/gin"}}, true},
		{"admin api client", Entry{Type: "feature", Description: "x", Modules: []string{"clients/adminapi"}}, true},
		{"bad type", Entry{Type: "nope", Description: "x", Modules: []string{"."}}, false},
		{"bad bump", Entry{Type: "feature", Bump: "huge", Description: "x", Modules: []string{"."}}, false},
		{"empty desc", Entry{Type: "feature", Description: "  ", Modules: []string{"."}}, false},
		{"no modules", Entry{Type: "feature", Description: "x"}, false},
		{"unknown module", Entry{Type: "feature", Description: "x", Modules: []string{"integrations/fiber"}}, false},
	}
	for _, tt := range tests {
		got := len(tt.entry.Validate()) == 0
		if got != tt.valid {
			t.Errorf("%s: valid=%v want %v (issues: %v)", tt.name, got, tt.valid, tt.entry.Validate())
		}
	}
}

func TestComputeReleases_MaxBumpPerModule(t *testing.T) {
	entries := []Entry{
		{Type: "bugfix", Description: "a", Modules: []string{"."}},  // patch
		{Type: "feature", Description: "b", Modules: []string{"."}}, // minor -> wins for core
		{Type: "bugfix", Description: "c", Modules: []string{"integrations/gin"}},
	}
	current := map[string]SemVer{
		".":                {1, 0, 0},
		"integrations/gin": {1, 0, 0},
	}
	releases, err := ComputeReleases(entries, current)
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 2 {
		t.Fatalf("got %d releases, want 2", len(releases))
	}
	// Core first (registry order), minor bump.
	if releases[0].Module.Dir != "." || releases[0].Next != (SemVer{1, 1, 0}) {
		t.Errorf("core release = %v %v", releases[0].Module.Dir, releases[0].Next)
	}
	if releases[1].Module.Dir != "integrations/gin" || releases[1].Next != (SemVer{1, 0, 1}) {
		t.Errorf("gin release = %v %v", releases[1].Module.Dir, releases[1].Next)
	}
}

func TestComputeReleases_DependencyOrder(t *testing.T) {
	// Entries listed dependents-first; releases must still come out in
	// dependency order: client, core, gin.
	entries := []Entry{
		{Type: "bugfix", Description: "g", Modules: []string{"integrations/gin"}},
		{Type: "feature", Description: "c", Modules: []string{"."}},
		{Type: "feature", Description: "a", Modules: []string{"clients/adminapi"}},
	}
	current := map[string]SemVer{
		"clients/adminapi": {1, 0, 0},
		".":                {1, 3, 0},
		"integrations/gin": {1, 3, 0},
	}
	releases, err := ComputeReleases(entries, current)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range releases {
		got = append(got, r.Tag())
	}
	want := []string{"clients/adminapi/v1.1.0", "v1.4.0", "integrations/gin/v1.3.1"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("tags = %v, want %v", got, want)
	}
}

// The tagging order relies on every module being listed after its
// dependencies in the registry.
func TestModules_RegistryFollowsDependencyOrder(t *testing.T) {
	seen := map[string]bool{}
	for _, m := range Modules {
		for _, dep := range m.DependsOn {
			if _, ok := moduleByDir(dep); !ok {
				t.Errorf("module %q depends on unknown module %q", m.Dir, dep)
			}
			if !seen[dep] {
				t.Errorf("module %q is listed before its dependency %q", m.Dir, dep)
			}
		}
		seen[m.Dir] = true
	}
}

func TestComputeReleases_MajorFails(t *testing.T) {
	entries := []Entry{{Type: "feature", Bump: "major", Description: "breaking", Modules: []string{"."}}}
	_, err := ComputeReleases(entries, map[string]SemVer{".": {1, 0, 0}})
	if err == nil {
		t.Fatal("expected error for major bump")
	}
	if !strings.Contains(err.Error(), "major") {
		t.Errorf("error %q should mention major", err)
	}
}

func TestComputeReleases_UntaggedModuleStartsAtZero(t *testing.T) {
	entries := []Entry{{Type: "feature", Description: "first", Modules: []string{"integrations/gin"}}}
	releases, err := ComputeReleases(entries, map[string]SemVer{}) // no current tags
	if err != nil {
		t.Fatal(err)
	}
	if len(releases) != 1 || releases[0].Next != (SemVer{0, 1, 0}) {
		t.Fatalf("untagged module minor bump = %v, want v0.1.0", releases[0].Next)
	}
}

func TestRenderNotes_MatchesModuleHighlightsFormat(t *testing.T) {
	entries := []Entry{
		{Type: "feature", Description: "Add slog-based request logging middleware.", Modules: []string{"."}},
		{Type: "bugfix", Description: "Fix HMAC validation for empty request bodies.", Modules: []string{"."}},
		{Type: "documentation", Description: "Document router group usage.", Modules: []string{"integrations/gin"}},
	}
	current := map[string]SemVer{".": {1, 0, 0}, "integrations/gin": {1, 0, 0}}
	releases, err := ComputeReleases(entries, current)
	if err != nil {
		t.Fatal(err)
	}
	got := RenderNotes(entries, releases, "")

	want := "## Module Highlights\n" +
		"* `github.com/shopware/sdk-go`: v1.1.0\n" +
		"  * **Feature**: Add slog-based request logging middleware.\n" +
		"  * **Bug Fix**: Fix HMAC validation for empty request bodies.\n" +
		"* `github.com/shopware/sdk-go/integrations/gin`: integrations/gin/v1.0.1\n" +
		"  * **Documentation**: Document router group usage.\n"

	if got != want {
		t.Errorf("notes mismatch:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestCheckDependencies(t *testing.T) {
	const (
		coreImport   = "github.com/shopware/sdk-go"
		clientImport = "github.com/shopware/sdk-go/clients/adminapi"
	)
	rel := func(dir string, next SemVer) Release {
		m, _ := moduleByDir(dir)
		return Release{Module: m, Next: next}
	}
	ginRelease := func(next SemVer) []Release {
		return []Release{rel("integrations/gin", next)}
	}
	coreAndGin := func(coreNext, ginNext SemVer) []Release {
		return []Release{rel(".", coreNext), rel("integrations/gin", ginNext)}
	}
	// req simulates the go.mod files: every module requires the given core
	// version and client v1.0.0 (already tagged in the cases that use it).
	req := func(core SemVer) RequiredVersionFunc {
		return reqMap(map[string]SemVer{coreImport: core, clientImport: {1, 0, 0}})
	}
	tagged := func(m map[string]SemVer) map[string]SemVer {
		m["clients/adminapi"] = SemVer{1, 0, 0}
		return m
	}

	tests := []struct {
		name     string
		releases []Release
		current  map[string]SemVer
		required RequiredVersionFunc
		wantErr  bool
	}{
		{
			name:     "gin only, requires already-tagged core",
			releases: ginRelease(SemVer{1, 0, 1}),
			current:  tagged(map[string]SemVer{".": {1, 0, 0}}),
			required: req(SemVer{1, 0, 0}), // <= current core -> ok
		},
		{
			name:     "gin only, requires untagged future core -> fail",
			releases: ginRelease(SemVer{1, 0, 1}),
			current:  tagged(map[string]SemVer{".": {1, 0, 0}}),
			required: req(SemVer{1, 1, 0}), // not tagged, core not releasing
			wantErr:  true,
		},
		{
			name:     "core+gin same run, gin requires the core being released",
			releases: coreAndGin(SemVer{1, 1, 0}, SemVer{1, 0, 1}),
			current:  tagged(map[string]SemVer{".": {1, 0, 0}, "integrations/gin": {1, 0, 0}}),
			required: req(SemVer{1, 1, 0}), // == core.Next -> ok
		},
		{
			name:     "core+gin same run, gin requires a different core version -> fail",
			releases: coreAndGin(SemVer{1, 1, 0}, SemVer{1, 0, 1}),
			current:  tagged(map[string]SemVer{".": {1, 0, 0}, "integrations/gin": {1, 0, 0}}),
			required: req(SemVer{1, 2, 0}), // neither tagged nor being released
			wantErr:  true,
		},
		{
			name:     "client+core same run, core requires the client being released",
			releases: []Release{rel("clients/adminapi", SemVer{1, 1, 0}), rel(".", SemVer{1, 4, 0})},
			current:  map[string]SemVer{"clients/adminapi": {1, 0, 0}, ".": {1, 3, 0}},
			required: reqMap(map[string]SemVer{clientImport: {1, 1, 0}}),
		},
		{
			name:     "core only, requires untagged future client -> fail",
			releases: []Release{rel(".", SemVer{1, 4, 0})},
			current:  map[string]SemVer{"clients/adminapi": {1, 0, 0}, ".": {1, 3, 0}},
			required: reqMap(map[string]SemVer{clientImport: {1, 1, 0}}),
			wantErr:  true,
		},
		{
			name:     "client+core same run, core pins a different client version -> fail",
			releases: []Release{rel("clients/adminapi", SemVer{1, 0, 1}), rel(".", SemVer{1, 4, 0})},
			current:  map[string]SemVer{"clients/adminapi": {1, 0, 0}, ".": {1, 3, 0}},
			required: reqMap(map[string]SemVer{clientImport: {1, 1, 0}}), // pinned ahead as minor, fragments give patch
			wantErr:  true,
		},
		{
			name: "all three in one run (pin ahead)",
			releases: []Release{
				rel("clients/adminapi", SemVer{1, 1, 0}), rel(".", SemVer{1, 4, 0}), rel("integrations/gin", SemVer{1, 4, 0}),
			},
			current:  map[string]SemVer{"clients/adminapi": {1, 0, 0}, ".": {1, 3, 0}, "integrations/gin": {1, 3, 0}},
			required: reqMap(map[string]SemVer{clientImport: {1, 1, 0}, coreImport: {1, 4, 0}}),
		},
		{
			name:     "gin only, requires untagged future client -> fail",
			releases: ginRelease(SemVer{1, 3, 1}),
			current:  map[string]SemVer{"clients/adminapi": {1, 0, 0}, ".": {1, 3, 0}, "integrations/gin": {1, 3, 0}},
			required: reqMap(map[string]SemVer{clientImport: {1, 1, 0}, coreImport: {1, 3, 0}}),
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		err := CheckDependencies(tt.releases, tt.current, tt.required)
		if (err != nil) != tt.wantErr {
			t.Errorf("%s: err=%v wantErr=%v", tt.name, err, tt.wantErr)
		}
	}
}

func TestRenderNotes_GeneralHighlightsOnlyFromInput(t *testing.T) {
	entries := []Entry{{Type: "feature", Description: "New API.", Modules: []string{"."}}}
	current := map[string]SemVer{".": {1, 0, 0}}
	releases, _ := ComputeReleases(entries, current)

	// With maintainer free text: section present, followed by Module Highlights.
	withGeneral := RenderNotes(entries, releases, "Some **free** text.")
	want := "## General Highlights\n\nSome **free** text.\n\n" +
		"## Module Highlights\n" +
		"* `github.com/shopware/sdk-go`: v1.1.0\n" +
		"  * **Feature**: New API.\n"
	if withGeneral != want {
		t.Errorf("with general text:\n--- got ---\n%s\n--- want ---\n%s", withGeneral, want)
	}

	// Without free text: no General Highlights section at all.
	plain := RenderNotes(entries, releases, "")
	if strings.Contains(plain, "General Highlights") {
		t.Errorf("General Highlights must be omitted when no text is provided:\n%s", plain)
	}
	if !strings.HasPrefix(plain, "## Module Highlights\n") {
		t.Errorf("Module Highlights must always be present and lead:\n%s", plain)
	}
}

// reqMap returns a RequiredVersionFunc that serves the same requires for every
// module directory; import paths not in the map are reported as not required.
func reqMap(m map[string]SemVer) RequiredVersionFunc {
	return func(dir, importPath string) (SemVer, bool, error) {
		v, ok := m[importPath]
		return v, ok, nil
	}
}
