package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const changelogDirName = ".changelog"

// changelogDir returns the absolute .changelog directory, anchored to the git
// repository root so the tool works regardless of the current directory.
func changelogDir() (string, error) {
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Join(strings.TrimSpace(root), changelogDirName), nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "new":
		err = cmdNew(os.Args[2:])
	case "validate":
		err = cmdValidate(os.Args[2:])
	case "plan":
		err = cmdPlan(os.Args[2:])
	case "gen":
		err = cmdGen(os.Args[2:])
	case "-h", "--help", "help":
		usage()
		return
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `changelog — manage release changelog fragments

Usage:
  changelog new [--type T --bump B --modules a,b --description "..."] [--major]
      Create a .changelog/<id>.json fragment. Interactive when flags are omitted
      (the interactive flow assumes a non-breaking change). --major forces a
      major bump (equivalent to --bump major).
  changelog validate [files...]
      Validate fragments (default: all under .changelog/). Used by CI.
  changelog plan [--since <ref>]
      Print the modules that would be released and their next versions.
  changelog gen [--since <ref>] [--general <file>]
      Print the GitHub Release body (Module Highlights) to stdout.

Fragment types: feature, bugfix, documentation, dependency, release
See docs/CHANGELOG_AND_RELEASES.md.
`)
}

// ---- new -------------------------------------------------------------------

func cmdNew(args []string) error {
	var typeF, bumpF, modulesF, descF string
	newFlagSet(args, map[string]*string{
		"type": &typeF, "bump": &bumpF, "modules": &modulesF, "description": &descF,
	})
	// --major is sugar for --bump major. The interactive flow never prompts for
	// it; breaking changes go through the dedicated `new --major` path.
	major := hasFlag(args, "major")

	interactive := typeF == "" && descF == "" && modulesF == ""
	if interactive {
		var err error
		if typeF, modulesF, descF, err = promptNew(); err != nil {
			return err
		}
	}
	if major {
		bumpF = "major"
	}

	mods := splitCSV(modulesF)
	e := Entry{Type: typeF, Bump: bumpF, Description: strings.TrimSpace(descF), Modules: mods}
	if issues := e.Validate(); len(issues) > 0 {
		return fmt.Errorf("invalid entry:\n\t%s", strings.Join(issues, "\n\t"))
	}

	id, err := newID()
	if err != nil {
		return err
	}
	dir, err := changelogDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, id+".json")
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return err
	}
	fmt.Println("created", path)
	return nil
}

func promptNew() (typeF, modulesF, descF string, err error) {
	r := bufio.NewReader(os.Stdin)

	var dirs []string
	for _, m := range Modules {
		dirs = append(dirs, m.Dir)
	}
	fmt.Printf("Module(s) [%s] (comma-separated): ", strings.Join(dirs, ", "))
	modulesF = readLine(r)

	fmt.Print("Type [feature, bugfix, documentation, dependency, release]: ")
	typeF = strings.TrimSpace(readLine(r))

	fmt.Print("Description: ")
	descF = readLine(r)
	return typeF, modulesF, descF, nil
}

func readLine(r *bufio.Reader) string {
	s, _ := r.ReadString('\n')
	return strings.TrimRight(s, "\r\n")
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// ---- validate --------------------------------------------------------------

func cmdValidate(args []string) error {
	files := args
	if len(files) == 0 {
		var err error
		if files, err = allFragmentFiles(); err != nil {
			return err
		}
	}
	var problems []string
	for _, f := range files {
		e, err := readEntry(f)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		for _, iss := range e.Validate() {
			problems = append(problems, fmt.Sprintf("%s: %s", f, iss))
		}
	}
	if len(problems) > 0 {
		return fmt.Errorf("invalid changelog fragments:\n\t%s", strings.Join(problems, "\n\t"))
	}
	fmt.Printf("ok: %d fragment(s) valid\n", len(files))
	return nil
}

// ---- plan ------------------------------------------------------------------

func cmdPlan(args []string) error {
	var since, format string
	newFlagSet(args, map[string]*string{"since": &since, "format": &format})

	entries, releases, err := load(since)
	if err != nil {
		return err
	}
	if len(releases) == 0 {
		return fmt.Errorf("nothing to release: no fragments since %s", describeSince(since))
	}

	if format == "json" {
		type jsonRelease struct {
			Dir        string `json:"dir"`
			ImportPath string `json:"importPath"`
			Tag        string `json:"tag"`
			Next       string `json:"next"`
			Bump       string `json:"bump"`
		}
		out := make([]jsonRelease, 0, len(releases))
		for _, r := range releases {
			out = append(out, jsonRelease{
				Dir: r.Module.Dir, ImportPath: r.Module.ImportPath,
				Tag: r.Tag(), Next: r.Next.String(), Bump: r.Bump.String(),
			})
		}
		b, err := json.Marshal(out)
		if err != nil {
			return err
		}
		fmt.Println(string(b))
		return nil
	}

	fmt.Printf("Pending fragments: %d\n", len(entries))
	fmt.Println("Modules to release:")
	for _, r := range releases {
		fmt.Printf("  %-40s %s -> %s (%s, %d entr%s)\n",
			r.Module.ImportPath, r.Current, r.Next, r.Bump, r.EntryCount, plural(r.EntryCount))
	}
	return nil
}

// ---- gen -------------------------------------------------------------------

func cmdGen(args []string) error {
	var since, generalFile string
	newFlagSet(args, map[string]*string{"since": &since, "general": &generalFile})

	entries, releases, err := load(since)
	if err != nil {
		return err
	}
	if len(releases) == 0 {
		return fmt.Errorf("nothing to release: no fragments since %s", describeSince(since))
	}
	var general string
	if generalFile != "" {
		b, err := os.ReadFile(generalFile)
		if err != nil {
			return err
		}
		general = string(b)
	}
	fmt.Print(RenderNotes(entries, releases, general))
	return nil
}

// ---- shared load -----------------------------------------------------------

// load gathers pending fragments (added since the given ref, or all when since
// is empty) and computes the releases against the current module tags.
func load(since string) ([]Entry, []Release, error) {
	files, err := fragmentsSince(since)
	if err != nil {
		return nil, nil, err
	}
	var entries []Entry
	var problems []string
	for _, f := range files {
		e, err := readEntry(f)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", f, err))
			continue
		}
		if iss := e.Validate(); len(iss) > 0 {
			problems = append(problems, fmt.Sprintf("%s: %s", f, strings.Join(iss, "; ")))
			continue
		}
		entries = append(entries, e)
	}
	if len(problems) > 0 {
		return nil, nil, fmt.Errorf("invalid fragments:\n\t%s", strings.Join(problems, "\n\t"))
	}

	current, err := currentVersions()
	if err != nil {
		return nil, nil, err
	}
	releases, err := ComputeReleases(entries, current)
	if err != nil {
		return nil, nil, err
	}
	if err := CheckDependencies(releases, current, requiredVersionFromGoMod); err != nil {
		return nil, nil, err
	}
	return entries, releases, nil
}

// requiredVersionFromGoMod reads <repo-root>/<dir>/go.mod and returns the
// version required for importPath, if present.
func requiredVersionFromGoMod(dir, importPath string) (SemVer, bool, error) {
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return SemVer{}, false, err
	}
	goMod := filepath.Join(strings.TrimSpace(root), dir, "go.mod")
	b, err := os.ReadFile(goMod)
	if err != nil {
		return SemVer{}, false, err
	}
	v, found, err := parseRequiredVersion(b, importPath)
	if err != nil {
		return SemVer{}, false, fmt.Errorf("%s: %w", goMod, err)
	}
	return v, found, nil
}

// parseRequiredVersion finds the version required for importPath in go.mod
// content, handling both the block form ("<path> vX.Y.Z" inside require (...))
// and the single-line form ("require <path> vX.Y.Z").
func parseRequiredVersion(content []byte, importPath string) (SemVer, bool, error) {
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		var verField string
		switch {
		case len(fields) >= 2 && fields[0] == importPath:
			verField = fields[1]
		case len(fields) >= 3 && fields[0] == "require" && fields[1] == importPath:
			verField = fields[2]
		default:
			continue
		}
		v, err := ParseSemVer(verField)
		if err != nil {
			return SemVer{}, false, fmt.Errorf("bad version for %s: %w", importPath, err)
		}
		return v, true, nil
	}
	return SemVer{}, false, nil
}

// ---- fragment IO -----------------------------------------------------------

func allFragmentFiles() ([]string, error) {
	dir, err := changelogDir()
	if err != nil {
		return nil, err
	}
	matches, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)
	return matches, nil
}

// fragmentsSince returns the absolute paths of fragment files added since ref.
// When ref is empty it falls back to every current fragment (first release).
func fragmentsSince(ref string) ([]string, error) {
	if ref == "" {
		return allFragmentFiles()
	}
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return nil, err
	}
	root = strings.TrimSpace(root)
	// Run the diff *at the repo root* with a repo-relative pathspec. This makes
	// the result independent of the caller's working directory.
	out, err := git("-C", root, "diff", "--diff-filter=A", "--name-only", ref+"..HEAD", "--", changelogDirName)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasSuffix(line, ".json") {
			files = append(files, filepath.Join(root, line)) // names are repo-root-relative
		}
	}
	sort.Strings(files)
	return files, nil
}

func readEntry(path string) (Entry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	dec := json.NewDecoder(strings.NewReader(string(b)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&e); err != nil {
		return Entry{}, fmt.Errorf("parse: %w", err)
	}
	return e, nil
}

// ---- git / versions --------------------------------------------------------

// currentVersions returns the highest existing tag version per module (zero
// value v0.0.0 for a module that was never tagged).
func currentVersions() (map[string]SemVer, error) {
	out, err := git("tag", "--list")
	if err != nil {
		return nil, err
	}
	tags := strings.Split(strings.TrimSpace(out), "\n")
	res := map[string]SemVer{}
	for _, m := range Modules {
		var vs []SemVer
		for _, t := range tags {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			rest, ok := stripModulePrefix(t, m)
			if !ok {
				continue
			}
			v, err := ParseSemVer(rest)
			if err != nil {
				continue
			}
			vs = append(vs, v)
		}
		if max, ok := MaxSemVer(vs); ok {
			res[m.Dir] = max
		}
	}
	return res, nil
}

// stripModulePrefix returns the semver portion of a tag if it belongs to the
// module. For core it must be a bare vX.Y.Z (no other prefix). For prefixed
// modules the tag must start with the prefix.
func stripModulePrefix(tag string, m Module) (string, bool) {
	if m.TagPrefix == "" {
		// Core: bare "vX.Y.Z", must not contain a slash (that would be another module).
		if strings.Contains(tag, "/") || !strings.HasPrefix(tag, "v") {
			return "", false
		}
		return tag, true
	}
	if !strings.HasPrefix(tag, m.TagPrefix) {
		return "", false
	}
	return strings.TrimPrefix(tag, m.TagPrefix), true
}

func git(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return out.String(), nil
}

// ---- small helpers ---------------------------------------------------------

// newFlagSet binds --name value / --name=value string flags without pulling in
// the flag package's usage noise. Unknown flags are ignored.
func newFlagSet(args []string, bind map[string]*string) map[string]*string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if !strings.HasPrefix(a, "--") {
			continue
		}
		name := strings.TrimPrefix(a, "--")
		var val string
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name, val = name[:eq], name[eq+1:]
		} else if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
			val = args[i+1]
			i++
		}
		if p, ok := bind[name]; ok {
			*p = val
		}
	}
	return bind
}

// hasFlag reports whether --name is present in args (as --name or --name=...).
func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == "--"+name || strings.HasPrefix(a, "--"+name+"=") {
			return true
		}
	}
	return false
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func describeSince(since string) string {
	if since == "" {
		return "repository start"
	}
	return since
}

func plural(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}
