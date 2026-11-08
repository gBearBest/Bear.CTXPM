package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"

	"github.com/gBearBest/Bear.CTXPM/cli/internal/engine"
	"github.com/gBearBest/Bear.CTXPM/cli/internal/manifest"
)

func TestRunUpdatePrintsPartialJSONAndReturnsError(t *testing.T) {
	var second atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/second" && second.Add(1) > 1 {
			http.Error(w, "unavailable", 503)
			return
		}
		_, _ = w.Write([]byte("new"))
	}))
	defer server.Close()
	root := t.TempDir()
	deps := []manifest.Resource{}
	for _, name := range []string{"first", "second", "third"} {
		deps = append(deps, manifest.Resource{Name: name, Type: "rule", Layout: manifest.LayoutFile, Path: ".ctxpm/dependencies/rules/" + name + ".md", Entry: name + ".md", Source: &manifest.Source{Type: "url", URL: server.URL + "/" + name, Entry: name + ".md"}, Version: "sha256:old"})
	}
	if _, err := manifest.Save(root, &manifest.Manifest{Version: manifest.CurrentManifestVersion, Project: manifest.Project{Name: "sample"}, Dependencies: deps}); err != nil {
		t.Fatal(err)
	}
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	err = runUpdate(engine.New(root), []string{"--all", "--json"})
	os.Stdout = previous
	_ = write.Close()
	data, readErr := io.ReadAll(read)
	_ = read.Close()
	if err == nil || readErr != nil {
		t.Fatalf("err=%v read=%v", err, readErr)
	}
	var result engine.UpdateResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("invalid json %q: %v", data, err)
	}
	if result.Status != "partial" || len(result.Applied) != 1 || len(result.Skipped) != 2 || result.Skipped[0].Status != "failed" || result.Skipped[1].Status != "not_run" {
		t.Fatalf("result=%+v", result)
	}
}
