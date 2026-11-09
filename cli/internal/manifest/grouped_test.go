package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const groupedFixture = `version: "2.0"
project: {name: grouped}
agents: [codex, claude-code]
# Keep this section comment.
dependencies:
  - source:
      type: git
      url: https://example.com/ai.git
      ref: main
    type: skill
    path: skills/{name}
    x-group: keep
    resources:
      - name: reviewer
        version: abc # pinned
        x-resource: retain
      - name: helper
        path: extras/helper
        version: def

  # Independent group stays byte-identical.
  - source: {type: git, url: 'https://example.com/rules.git'}
    type: rule
    path: rules/{name}.md
    resources:
      - name: security
        version: xyz

packages:
  - name: create-moment
    type: skill
  - name: local-policy
    type: rule
    layout: file
    entry: policy.md
x-project: untouched
`

func TestGroupedExpandAndRoundTrip(t *testing.T) {
	m, err := Parse([]byte(groupedFixture))
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Source.Path != "skills/reviewer" || m.Dependencies[1].Source.Path != "extras/helper" {
		t.Fatal(m.Dependencies)
	}
	if got := m.Dependencies[2].Path; got != ".ctxpm/dependencies/rules/security.md" {
		t.Fatal(got)
	}
	if got := m.Packages[1].Path; got != ".ctxpm/packages/rules/policy.md" {
		t.Fatal(got)
	}
	data, err := Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != groupedFixture {
		t.Fatalf("no-op changed config:\n%s", data)
	}
	root := t.TempDir()
	writeManifest(t, root, groupedFixture)
	if _, err := UpdateResourceVersions(root, map[string]string{"reviewer": "new"}); err != nil {
		t.Fatal(err)
	}
	if got := readManifest(t, root); got != strings.Replace(groupedFixture, "version: abc # pinned", "version: new # pinned", 1) {
		t.Fatalf("version edit changed other text:\n%s", got)
	}
}
func TestGroupedAddRemoveAndFullSave(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, groupedFixture)
	m, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := m.Dependencies[0]
	r.Name = "third"
	r.Path = ".ctxpm/dependencies/skills/third"
	s := *r.Source
	s.Path = "skills/third"
	r.Source = &s
	if _, err = AddDependency(root, r); err != nil {
		t.Fatal(err)
	}
	content := readManifest(t, root)
	untouched := strings.Split(groupedFixture, "  # Independent group")[1]
	if !strings.HasSuffix(content, "  # Independent group"+untouched) {
		t.Fatalf("unrelated group changed:\n%s", content)
	}
	for _, want := range []string{"x-group: keep", "x-resource: retain", "name: third"} {
		if !strings.Contains(content, want) {
			t.Fatal(want, content)
		}
	}
	m, _, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	m.Agents = append(m.Agents, "cursor")
	if _, err = Save(root, m); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"third", "reviewer", "helper"} {
		if _, err = RemoveDependency(root, name); err != nil {
			t.Fatal(err)
		}
	}
	m, _, err = Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Dependencies) != 1 {
		t.Fatal(m.Dependencies)
	}
	if _, err = RemoveDependency(root, "security"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(readManifest(t, root), "dependencies: []") {
		t.Fatal(readManifest(t, root))
	}
}
func TestGroupedValidation(t *testing.T) {
	for _, tc := range []struct{ name, old, new string }{
		{"empty path", "path: extras/helper", `path: ""`},
		{"unknown template", "skills/{name}", "skills/{other}"},
		{"traversal", "extras/helper", "../helper"},
		{"duplicate name", "name: helper", "name: reviewer"},
		{"same filename", "extras/helper", "skills/reviewer"},
		{"invalid name", "name: helper", "name: ../helper"},
		{"resource source override", "path: extras/helper", "source: {ref: other}"},
		{"group version", "    path: skills/{name}", "    path: skills/{name}\n    version: shared"},
		{"package source", "    entry: policy.md", "    entry: policy.md\n    source: {type: url, url: https://example.com/policy.md}"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			data := strings.Replace(groupedFixture, tc.old, tc.new, 1)
			// Equal directory source paths remain separate local directories; use file collision instead.
			if tc.name == "same filename" {
				data = strings.Replace(data, "name: helper\n        path: skills/reviewer", "name: helper\n        type: rule\n        path: elsewhere/security.md", 1)
			}
			if _, err := Parse([]byte(data)); err == nil {
				t.Fatal("accepted invalid manifest")
			}
		})
	}
}

func TestConvertGroupedMissingSectionsAndPackageVersions(t *testing.T) {
	for _, raw := range []string{
		"project: {name: empty}\n",
		"version: 1.0\nproject: {name: empty}\ndependencies: []\n",
		"version: 1.0\nproject: {name: empty}\npackages: []\n",
		"version: 1.0\nproject: {name: local}\npackages:\n  - name: notes\n    type: memory\n    path: .ctxpm/packages/memories/notes\n    version: recorded\n",
	} {
		m, err := Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		data, err := ConvertGrouped(m)
		if err != nil {
			t.Fatal(err)
		}
		next, err := Parse(data)
		if err != nil {
			t.Fatalf("%v\n%s", err, data)
		}
		if err := EquivalentResources(m, next); err != nil {
			t.Fatal(err)
		}
	}
}

func TestConvertGroupedFilePackageWithImplicitEntry(t *testing.T) {
	for _, layout := range []string{"", "    layout: file\n"} {
		raw := "version: '1.0'\nproject: {name: implicit-entry}\ndependencies: []\npackages:\n  - name: local-policy\n    type: rule\n" + layout + "    path: .ctxpm/packages/rules/policy.md\n"
		m, err := Parse([]byte(raw))
		if err != nil {
			t.Fatal(err)
		}
		data, err := ConvertGrouped(m)
		if err != nil {
			t.Fatal(err)
		}
		next, err := Parse(data)
		if err != nil {
			t.Fatal(err)
		}
		if err := EquivalentResources(m, next); err != nil {
			t.Fatal(err)
		}
		if next.Packages[0].Entry != "policy.md" || !strings.Contains(string(data), "entry: policy.md") {
			t.Fatalf("derived entry missing:\n%s", data)
		}
	}
}

func TestGroupedEmptySourceFilesPreserveOriginalNodes(t *testing.T) {
	raw := strings.Replace(groupedFixture, "      ref: main\n", "      ref: main\n      files: [] # deliberately empty\n", 1)
	m, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(m)
	if err != nil || string(data) != raw {
		t.Fatalf("no-op rewrote original group: %v\n%s", err, data)
	}
	// Automatic discovery uses a full Save when it registers another package.
	m.Packages = append(m.Packages, Resource{Name: "new-rule", Type: "rule", Layout: LayoutFile, Path: ".ctxpm/packages/rules/new-rule.md", Entry: "new-rule.md"})
	root := t.TempDir()
	if _, err := Save(root, m); err != nil {
		t.Fatal(err)
	}
	got := readManifest(t, root)
	beforeDependencies := strings.Split(strings.Split(raw, "dependencies:\n")[1], "packages:\n")[0]
	afterDependencies := strings.Split(strings.Split(got, "dependencies:\n")[1], "packages:\n")[0]
	if beforeDependencies != afterDependencies {
		t.Fatalf("unrelated group metadata changed:\n%s", got)
	}
	if _, _, err := Load(root); err != nil {
		t.Fatal(err)
	}
}

func TestGroupedFlowAndBlockScalarVersionEdits(t *testing.T) {
	for _, resources := range []string{
		"[{name: first, version: a}, {name: second, version: b}]",
		"[{name: first}, {name: second, version: b}]",
		"\n      - name: first\n        version: |-\n          a\n      - name: second\n        version: b",
	} {
		root := t.TempDir()
		raw := "version: '2.0'\nproject: {name: versions}\ndependencies:\n  - source: {type: git, url: https://example.com/repo.git}\n    type: skill\n    resources: " + resources + "\npackages: []\n"
		writeManifest(t, root, raw)
		if _, err := UpdateResourceVersions(root, map[string]string{"first": "longer-first-version", "second": "longer-second-version"}); err != nil {
			t.Fatal(err)
		}
		m, _, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if m.Dependencies[0].Version != "longer-first-version" || m.Dependencies[1].Version != "longer-second-version" {
			t.Fatalf("unexpected versions: %+v", m.Dependencies)
		}
	}
}
func TestGroupedFilesAndURL(t *testing.T) {
	data := `version: "2.0"
project: {name: test}
dependencies:
  - source: {type: git, url: https://example.com/repo.git}
    type: rule
    resources:
      - name: notice
        path: NOTICE
        layout: file
        version: a
      - name: dotted
        path: rules/v1.0
        layout: dir
        entry: GUIDE.md
        version: b
  - source: {type: url, url: https://example.com/policy.txt}
    type: rule
    resources:
      - name: remote-policy
        version: 'sha256:abcd'
  - source:
      type: url
      url: https://example.com/memory/
      entry: MEMORY.md
      files: [MEMORY.md, notes.md]
    type: memory
    resources:
      - name: notes
        version: 'sha256tree:abcd'
packages: []
`
	m, err := Parse([]byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Path != ".ctxpm/dependencies/rules/NOTICE" || m.Dependencies[1].EffectiveLayout() != LayoutDir || m.Dependencies[2].Path != ".ctxpm/dependencies/rules/policy.txt" {
		t.Fatal(m.Dependencies)
	}
	if _, err := Marshal(m); err != nil {
		t.Fatal(err)
	}
}
func TestConvertGroupedPreservesSemanticsAndExtensions(t *testing.T) {
	original := `version: 1.0
project: {name: old, team: retained}
agents: [codex]
x-root: keep
dependencies:
  - name: first
    type: skill
    layout: dir
    path: .ctxpm/dependencies/skills/first
    entry: SKILL.md
    source: {type: git, url: https://example.com/repo.git, path: skills/first, ref: main, entry: SKILL.md}
    version: abc # important
    x-extra: preserve
  - name: second
    type: skill
    layout: dir
    path: .ctxpm/dependencies/skills/second
    entry: SKILL.md
    source: {type: git, url: https://example.com/repo.git, path: skills/second, ref: main, entry: SKILL.md}
    version: def
packages:
  - name: policy
    type: rule
    layout: file
    path: .ctxpm/packages/rules/policy.md
    entry: policy.md
`
	m, err := Parse([]byte(original))
	if err != nil {
		t.Fatal(err)
	}
	data, err := ConvertGrouped(m)
	if err != nil {
		t.Fatal(err)
	}
	next, err := Parse(data)
	if err != nil {
		t.Fatalf("%v\n%s", err, data)
	}
	if err = EquivalentResources(m, next); err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "url:") != 1 {
		t.Fatalf("source not grouped:\n%s", data)
	}
	for _, want := range []string{"x-root: keep", "x-extra: preserve", "# important", "team: retained"} {
		if !strings.Contains(string(data), want) {
			t.Fatal(want, string(data))
		}
	}
	again, err := ConvertGrouped(next)
	if err != nil || string(again) != string(data) {
		t.Fatal("not idempotent", err)
	}
	m.Dependencies[0].Path = ".ctxpm/dependencies/skills/custom"
	if _, err = ConvertGrouped(m); err == nil {
		t.Fatal("custom target silently dropped")
	}
}
func TestGroupedVersionInsertAndDerivedTargetPreflight(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, strings.Replace(groupedFixture, "        version: abc # pinned\n", "", 1))
	if _, err := UpdateResourceVersions(root, map[string]string{"reviewer": "new"}); err != nil {
		t.Fatal(err)
	}
	m, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if m.Dependencies[0].Version != "new" {
		t.Fatal("version not inserted")
	}
	before, _ := os.ReadFile(filepath.Join(root, "ctxpm.yaml"))
	m.Dependencies[0].Path = ".ctxpm/dependencies/custom"
	if _, err = Save(root, m); err == nil {
		t.Fatal("custom path saved")
	}
	after, _ := os.ReadFile(filepath.Join(root, "ctxpm.yaml"))
	if string(before) != string(after) {
		t.Fatal("failed save changed config")
	}
}

func TestGroupedPortableFileAndAgentCollisions(t *testing.T) {
	for _, tc := range []struct{ name, fragment string }{
		{"basename", "      - name: second\n        path: other/security.md\n        version: def\n"},
		{"case", "      - name: second\n        path: other/SECURITY.md\n        version: def\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := `version: "2.0"
project: {name: collisions}
agents: [generic]
dependencies:
  - source: {type: git, url: https://example.com/repo.git}
    type: rule
    path: rules/{name}.md
    resources:
      - name: security
        version: abc
` + tc.fragment + "packages: []\n"
			if _, err := Parse([]byte(raw)); err == nil {
				t.Fatal("file collision accepted")
			}
		})
	}
	// Dependency/package local directories differ, but discovery paths coincide.
	raw := strings.Replace(groupedFixture, "name: create-moment", "name: security", 1)
	raw = strings.Replace(raw, "name: security\n    type: skill", "name: security\n    type: rule\n    layout: file\n    entry: security.md", 1)
	// Use a distinct logical name to exercise compatibility collision, not name duplication.
	raw = strings.Replace(raw, "name: security\n    type: rule", "name: package-security\n    type: rule", 1)
	if _, err := Parse([]byte(raw)); err == nil {
		t.Fatal("agent link collision accepted")
	}
}
func TestGroupedCRLFAndMissingVersion(t *testing.T) {
	root := t.TempDir()
	raw := strings.ReplaceAll(groupedFixture, "\n", "\r\n")
	writeManifest(t, root, raw)
	m, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	data, err := Marshal(m)
	if err != nil || string(data) != raw {
		t.Fatal("CRLF no-op changed config", err)
	}
	if _, err := UpdateResourceVersions(root, map[string]string{"helper": "fresh"}); err != nil {
		t.Fatal(err)
	}
	expected := strings.Replace(raw, "version: def", "version: fresh", 1)
	if readManifest(t, root) != expected {
		t.Fatal("CRLF version edit changed layout")
	}
}

func TestGroupedFlowSectionsCanBeEdited(t *testing.T) {
	raw := `version: "2.0"
project: {name: inline}
agents: [generic]
dependencies: [{source: {type: git, url: https://example.com/repo.git}, type: skill, path: 'skills/{name}', resources: [{name: first, version: abc}]}]
packages: []
`
	root := t.TempDir()
	writeManifest(t, root, raw)
	m, _, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	r := m.Dependencies[0]
	r.Name = "second"
	r.Path = ".ctxpm/dependencies/skills/second"
	source := *r.Source
	source.Path = "skills/second"
	r.Source = &source
	if _, err := AddDependency(root, r); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveDependency(root, "first"); err != nil {
		t.Fatal(err)
	}
	m, _, err = Load(root)
	if err != nil || len(m.Dependencies) != 1 || m.Dependencies[0].Name != "second" {
		t.Fatal(m, err)
	}
}
