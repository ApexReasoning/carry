//go:build live

package pi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestLivePiReferenceExtensionLoadsAndExecutesWithPinnedRuntime(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		requests.Add(1)
		if request.Method != http.MethodGet || request.URL.EscapedPath() != "/v1/references/native-contract" {
			t.Errorf("request = %s %s", request.Method, request.URL.EscapedPath())
			return
		}
		_, _ = response.Write([]byte("native reference"))
	}))
	defer server.Close()

	directory := t.TempDir()
	extensionPath, err := writeReferenceExtension(directory, server.URL)
	if err != nil {
		t.Fatalf("write Reference extension: %v", err)
	}
	packageRoot := installedPiPackageRoot(t)
	program := `
import { pathToFileURL } from "node:url";

const packageRoot = process.argv[2];
const extensionPath = process.argv[3];
const loader = await import(pathToFileURL(packageRoot + "/dist/core/extensions/loader.js").href);
const loaded = await loader.loadExtensions([extensionPath], process.cwd());
if (loaded.errors.length !== 0) throw new Error(JSON.stringify(loaded.errors));
const registered = loaded.extensions.flatMap((extension) => [...extension.tools.values()])
  .find((tool) => tool.definition.name === "lookup_reference");
if (!registered) throw new Error("lookup_reference was not registered");
if (registered.definition.parameters.additionalProperties !== false) {
  throw new Error("lookup_reference schema permits additional properties");
}
const result = await registered.definition.execute(
  "native-call",
  { key: "native-contract" },
  undefined,
  undefined,
  {},
);
let rejectedExtra = false;
try {
  await registered.definition.execute(
    "extra-call",
    { key: "native-contract", url: "https://attacker.example" },
    undefined,
    undefined,
    {},
  );
} catch {
  rejectedExtra = true;
}
process.stdout.write(JSON.stringify({
  text: result.content?.[0]?.text,
  rejectedExtra,
}));
`
	programPath := filepath.Join(directory, "load-reference-extension.mjs")
	if err := os.WriteFile(programPath, []byte(program), 0o600); err != nil {
		t.Fatalf("write pinned Pi loader canary: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "node", programPath, packageRoot, extensionPath).CombinedOutput()
	if err != nil {
		t.Fatalf("load Reference extension with pinned Pi: %v: %s", err, output)
	}
	var result struct {
		Text          string `json:"text"`
		RejectedExtra bool   `json:"rejectedExtra"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		t.Fatalf("decode pinned Pi contract output %q: %v", output, err)
	}
	if result.Text != "native reference" || !result.RejectedExtra || requests.Load() != 1 {
		t.Fatalf("pinned Pi contract = %#v, requests = %d", result, requests.Load())
	}
}

func installedPiPackageRoot(t *testing.T) string {
	t.Helper()
	binary, err := exec.LookPath("pi")
	if err != nil {
		t.Fatalf("find pinned Pi: %v", err)
	}
	shim, err := os.ReadFile(binary)
	if err != nil {
		t.Fatalf("read Pi launcher %s: %v", binary, err)
	}
	const marker = "# cmd-shim-target="
	index := strings.LastIndex(string(shim), marker)
	if index < 0 {
		t.Fatalf("Pi launcher %s does not expose its package target", binary)
	}
	target := strings.TrimSpace(string(shim[index+len(marker):]))
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(binary), target)
	}
	packageRoot := filepath.Dir(filepath.Dir(filepath.Clean(target)))
	manifest, err := os.ReadFile(filepath.Join(packageRoot, "package.json"))
	if err != nil {
		t.Fatalf("read pinned Pi package manifest: %v", err)
	}
	var metadata struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(manifest, &metadata) != nil || metadata.Version != "0.84.2" {
		t.Fatalf("Pi package version = %q, want 0.84.2", metadata.Version)
	}
	return packageRoot
}
