package projecttype_test

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/richardwooding/file-search-on/internal/projecttype"
)

func TestDetect_SingleType(t *testing.T) {
	cases := []struct {
		name     string
		indicator string
		wantType string
	}{
		{"go", "go.mod", "go"},
		{"node", "package.json", "node"},
		{"rust", "Cargo.toml", "rust"},
		{"python-pyproject", "pyproject.toml", "python"},
		{"python-requirements", "requirements.txt", "python"},
		{"python-pipfile", "Pipfile", "python"},
		{"ruby", "Gemfile", "ruby"},
		{"java-maven", "pom.xml", "java-maven"},
		{"java-gradle", "build.gradle.kts", "java-gradle"},
		{"docker-compose", "compose.yaml", "docker-compose"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.indicator), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			matches := projecttype.Detect(nil, dir)
			if len(matches) != 1 || matches[0].Type != tc.wantType {
				t.Fatalf("Detect: got %+v, want single match for %q", matches, tc.wantType)
			}
			if matches[0].Indicator != tc.indicator {
				t.Errorf("indicator = %q, want %q", matches[0].Indicator, tc.indicator)
			}
		})
	}
}

func TestDetect_GlobIndicators(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		wantType string
	}{
		{"terraform", "main.tf", "terraform"},
		{"terraform-variants", "providers.tf", "terraform"},
		{"dotnet-csproj", "MyApp.csproj", "dotnet"},
		{"dotnet-fsproj", "MyApp.fsproj", "dotnet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, tc.file), []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
			matches := projecttype.Detect(nil, dir)
			if len(matches) != 1 || matches[0].Type != tc.wantType {
				t.Fatalf("Detect: got %+v, want single match for %q", matches, tc.wantType)
			}
		})
	}
}

func TestDetect_MultipleTypes(t *testing.T) {
	// A Go module that also ships a docker-compose.yml fires both
	// — matches the cross-firing semantics established in PR #95.
	dir := t.TempDir()
	for _, f := range []string{"go.mod", "docker-compose.yml"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	matches := projecttype.Detect(nil, dir)
	got := make([]string, len(matches))
	for i, m := range matches {
		got[i] = m.Type
	}
	sort.Strings(got)
	want := []string{"docker-compose", "go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Detect: got %v, want %v", got, want)
	}
}

func TestDetect_NoMatch(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "random.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if matches := projecttype.Detect(nil, dir); len(matches) != 0 {
		t.Errorf("Detect: got %+v, want empty", matches)
	}
}

func TestFind_StopsAtProjectRoot(t *testing.T) {
	root := t.TempDir()
	// root/a/go.mod  → a is a project root
	// root/a/inner/go.mod  → would be a project but Find should stop at /a
	mustMkdir(t, filepath.Join(root, "a"))
	mustMkdir(t, filepath.Join(root, "a", "inner"))
	mustWrite(t, filepath.Join(root, "a", "go.mod"), "module a\n")
	mustWrite(t, filepath.Join(root, "a", "inner", "go.mod"), "module a/inner\n")

	result, err := projecttype.Find(t.Context(), root, projecttype.FindOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 {
		t.Fatalf("got %d projects, want 1; projects=%+v", result.Count, result.Projects)
	}
	if got := result.Projects[0].Path; got != filepath.Join(root, "a") {
		t.Errorf("path = %q, want a", got)
	}
}

func TestFind_NestedTrue(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "a"))
	mustMkdir(t, filepath.Join(root, "a", "inner"))
	mustWrite(t, filepath.Join(root, "a", "go.mod"), "module a\n")
	mustWrite(t, filepath.Join(root, "a", "inner", "Cargo.toml"), "[package]\nname=\"x\"\n")

	result, err := projecttype.Find(t.Context(), root, projecttype.FindOptions{Nested: true})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 {
		t.Fatalf("got %d projects, want 2; projects=%+v", result.Count, result.Projects)
	}
}

func TestFind_TypesFilter(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "go-app"))
	mustMkdir(t, filepath.Join(root, "rust-app"))
	mustMkdir(t, filepath.Join(root, "node-app"))
	mustWrite(t, filepath.Join(root, "go-app", "go.mod"), "module x\n")
	mustWrite(t, filepath.Join(root, "rust-app", "Cargo.toml"), "[package]\nname=\"x\"\n")
	mustWrite(t, filepath.Join(root, "node-app", "package.json"), `{"name":"x"}`)

	result, err := projecttype.Find(t.Context(), root, projecttype.FindOptions{
		Types: []string{"go", "rust"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 2 {
		t.Fatalf("got %d projects, want 2; projects=%+v", result.Count, result.Projects)
	}
}

func TestFind_Excludes(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "real"))
	mustMkdir(t, filepath.Join(root, "node_modules"))
	mustMkdir(t, filepath.Join(root, "node_modules", "vendored"))
	mustWrite(t, filepath.Join(root, "real", "go.mod"), "module real\n")
	mustWrite(t, filepath.Join(root, "node_modules", "vendored", "go.mod"), "module vendored\n")

	result, err := projecttype.Find(t.Context(), root, projecttype.FindOptions{
		Excludes: []string{"node_modules"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Count != 1 {
		t.Fatalf("got %d projects, want 1 (node_modules excluded); projects=%+v", result.Count, result.Projects)
	}
}

func TestRegistry_Types(t *testing.T) {
	types := projecttype.DefaultRegistry().Types()
	if len(types) < 10 {
		t.Errorf("registered types = %d, want at least 10 built-ins", len(types))
	}
	// Verify sorted by Name.
	for i := 1; i < len(types); i++ {
		if types[i-1].Name > types[i].Name {
			t.Errorf("types not sorted: %s > %s", types[i-1].Name, types[i].Name)
		}
	}
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
