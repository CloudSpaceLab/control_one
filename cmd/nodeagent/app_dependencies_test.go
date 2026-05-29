package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/internal/api"
)

func TestCollectAppDependenciesParsesCommonManifests(t *testing.T) {
	root := t.TempDir()
	app := filepath.Join(root, "srv", "core-api")
	mustMkdir(t, app)
	mustWriteFile(t, filepath.Join(app, "package-lock.json"), `{
  "lockfileVersion": 3,
  "packages": {
    "": {"name": "core-api"},
    "node_modules/express": {"version": "4.18.2", "license": "MIT"},
    "node_modules/@scope/widget": {"version": "1.2.3"},
    "node_modules/dev-only": {"version": "9.9.9", "dev": true}
  }
}`)
	mustWriteFile(t, filepath.Join(app, "package.json"), `{
  "dependencies": {"lodash": "4.17.21", "range-only": "^1.0.0"},
  "devDependencies": {"jest": "29.0.0"}
}`)
	mustWriteFile(t, filepath.Join(app, "requirements.txt"), "requests==2.31.0\npytest==8.0.0 # intentionally treated as production without a dev file\n")
	mustWriteFile(t, filepath.Join(app, "go.mod"), `module example.com/core

require (
	github.com/gin-gonic/gin v1.9.1
	golang.org/x/crypto v0.23.0 // indirect
)
`)
	mustWriteFile(t, filepath.Join(app, "core.csproj"), `<Project>
  <ItemGroup>
    <PackageReference Include="Newtonsoft.Json" Version="13.0.3" />
  </ItemGroup>
</Project>`)
	mustWriteFile(t, filepath.Join(app, "pom.xml"), `<project>
  <dependencies>
    <dependency>
      <groupId>org.apache.logging.log4j</groupId>
      <artifactId>log4j-core</artifactId>
      <version>2.17.1</version>
    </dependency>
    <dependency>
      <groupId>junit</groupId>
      <artifactId>junit</artifactId>
      <version>4.13.2</version>
      <scope>test</scope>
    </dependency>
  </dependencies>
</project>`)
	mustWriteFile(t, filepath.Join(app, "sbom.cdx.json"), `{
  "bomFormat": "CycloneDX",
  "components": [
    {"type":"library","name":"urllib3","version":"2.2.1","purl":"pkg:pypi/urllib3@2.2.1","licenses":[{"license":{"id":"MIT"}}]}
  ]
}`)

	deps, err := collectAppDependencies(appDependencyScanOptions{
		ScanRoots:    []string{filepath.Join(root, "srv")},
		MaxDepth:     6,
		MaxManifests: 64,
		MaxFileBytes: 1 << 20,
	})
	if err != nil {
		t.Fatalf("collect app dependencies: %v", err)
	}
	byKey := appDepByKey(deps)
	assertAppDep(t, byKey, "npm/express", "4.18.2", "pkg:npm/express@4.18.2")
	assertAppDep(t, byKey, "npm/@scope/widget", "1.2.3", "pkg:npm/%40scope/widget@1.2.3")
	assertAppDep(t, byKey, "npm/lodash", "4.17.21", "pkg:npm/lodash@4.17.21")
	assertAppDep(t, byKey, "pypi/requests", "2.31.0", "pkg:pypi/requests@2.31.0")
	assertAppDep(t, byKey, "go/github.com/gin-gonic/gin", "v1.9.1", "pkg:golang/github.com/gin-gonic/gin@v1.9.1")
	assertAppDep(t, byKey, "go/golang.org/x/crypto", "v0.23.0", "pkg:golang/golang.org/x/crypto@v0.23.0")
	assertAppDep(t, byKey, "nuget/Newtonsoft.Json", "13.0.3", "pkg:nuget/Newtonsoft.Json@13.0.3")
	assertAppDep(t, byKey, "maven/org.apache.logging.log4j:log4j-core", "2.17.1", "pkg:maven/org.apache.logging.log4j/log4j-core@2.17.1")
	assertAppDep(t, byKey, "pypi/urllib3", "2.2.1", "pkg:pypi/urllib3@2.2.1")
	if _, ok := byKey["npm/dev-only"]; ok {
		t.Fatalf("dev-only package was included with include_dev_dependencies=false")
	}
	if _, ok := byKey["maven/junit:junit"]; ok {
		t.Fatalf("test-scoped Maven package was included with include_dev_dependencies=false")
	}
	if got := byKey["npm/range-only"].Version; got != "" {
		t.Fatalf("range-only version = %q, want empty because package.json range is not an installed version", got)
	}
}

func TestPostAppDependenciesUsesNodeEndpoint(t *testing.T) {
	var gotPath string
	var gotPayload appDependenciesPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotPayload)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client, err := api.NewClient(server.URL, "", "", "", "")
	if err != nil {
		t.Fatalf("api.NewClient: %v", err)
	}
	err = postAppDependencies(context.Background(), client, zap.NewNop(), "node-1", []AppDependencyInfo{{
		AppRoot:   "/srv/core-api",
		Ecosystem: "npm",
		Name:      "express",
		Version:   "4.18.2",
	}})
	if err != nil {
		t.Fatalf("post app dependencies: %v", err)
	}
	if gotPath != "/api/v1/nodes/node-1/app-dependencies" {
		t.Fatalf("path = %q", gotPath)
	}
	if len(gotPayload.Dependencies) != 1 || gotPayload.Dependencies[0].Name != "express" {
		t.Fatalf("payload = %#v", gotPayload)
	}
}

func appDepByKey(deps []AppDependencyInfo) map[string]AppDependencyInfo {
	out := map[string]AppDependencyInfo{}
	for _, dep := range deps {
		key := dep.Ecosystem + "/" + dep.Name
		if existing, ok := out[key]; ok && existing.Version != "" {
			continue
		}
		out[key] = dep
	}
	return out
}

func assertAppDep(t *testing.T, deps map[string]AppDependencyInfo, key, version, purl string) {
	t.Helper()
	dep, ok := deps[key]
	if !ok {
		t.Fatalf("missing dependency %s in %#v", key, deps)
	}
	if dep.Version != version {
		t.Fatalf("%s version = %q, want %q", key, dep.Version, version)
	}
	if dep.PURL != purl {
		t.Fatalf("%s purl = %q, want %q", key, dep.PURL, purl)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
