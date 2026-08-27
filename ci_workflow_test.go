package main

import (
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

type workflow struct {
	On          map[string]any         `yaml:"on"`
	Permissions map[string]string      `yaml:"permissions"`
	Jobs        map[string]workflowJob `yaml:"jobs"`
}

type workflowJob struct {
	RunsOn string         `yaml:"runs-on"`
	Steps  []workflowStep `yaml:"steps"`
}

type workflowStep struct {
	Name             string         `yaml:"name"`
	Uses             string         `yaml:"uses"`
	Run              string         `yaml:"run"`
	WorkingDirectory string         `yaml:"working-directory"`
	With             map[string]any `yaml:"with"`
}

func TestCIWorkflowContract(t *testing.T) {
	b, err := os.ReadFile(".github/workflows/ci.yml")
	if err != nil {
		t.Fatal(err)
	}
	var got workflow
	if err := yaml.Unmarshal(b, &got); err != nil {
		t.Fatalf("parse workflow YAML: %v", err)
	}
	if len(got.On) != 1 {
		t.Fatalf("expected exactly pull_request trigger, got %#v", got.On)
	}
	if _, ok := got.On["pull_request"]; !ok {
		t.Fatalf("expected pull_request trigger, got %#v", got.On)
	}
	if !reflect.DeepEqual(got.Permissions, map[string]string{"contents": "read"}) {
		t.Fatalf("unexpected permissions: %#v", got.Permissions)
	}
	if len(got.Jobs) != 1 {
		t.Fatalf("expected one job, got %d", len(got.Jobs))
	}
	job, ok := got.Jobs["build-and-test"]
	if !ok || job.RunsOn != "ubuntu-latest" {
		t.Fatalf("unexpected job contract: %#v", got.Jobs)
	}
	expectedNames := []string{"Check out source", "Set up Go", "Verify clean-checkout root tests", "Verify repository-wide known fixture failures", "Set up pnpm", "Set up Node.js", "Build frontend from lockfile", "Verify production embedding"}
	if len(job.Steps) != len(expectedNames) {
		t.Fatalf("expected %d steps, got %d", len(expectedNames), len(job.Steps))
	}
	for i, name := range expectedNames {
		if job.Steps[i].Name != name {
			t.Errorf("step %d: expected %q, got %q", i, name, job.Steps[i].Name)
		}
	}
	expectedActions := map[string]string{
		"Check out source": "actions/checkout@11d5960a326750d5838078e36cf38b85af677262",
		"Set up Go":        "actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16",
		"Set up pnpm":      "pnpm/action-setup@b906affcce14559ad1aafd4ab0e942779e9f58b1",
		"Set up Node.js":   "actions/setup-node@49933ea5288caeca8642d1e84afbd3f7d6820020",
	}
	pin := regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
	for _, step := range job.Steps {
		if want, action := expectedActions[step.Name]; action {
			if step.Uses != want || !pin.MatchString(step.Uses) {
				t.Errorf("%s action is not the expected immutable pin: %q", step.Name, step.Uses)
			}
		} else if step.Uses != "" {
			t.Errorf("unexpected action in %s: %q", step.Name, step.Uses)
		}
	}
	if v, ok := job.Steps[0].With["persist-credentials"].(bool); !ok || v {
		t.Fatalf("checkout must disable credential persistence: %#v", job.Steps[0].With)
	}
	if job.Steps[1].With["go-version"] != "1.25.6" || job.Steps[4].With["version"] != "10.30.1" || job.Steps[5].With["node-version"] != "22.12.0" {
		t.Fatalf("unexpected tool versions")
	}
	expectedRuns := map[string][]string{
		"Verify clean-checkout root tests":              {"go test . -count=1"},
		"Verify repository-wide known fixture failures": {"go test -json ./... -count=1", "go run ./cmd/verify-known-fixtures"},
		"Build frontend from lockfile":                  {"pnpm install --frozen-lockfile", "pnpm build"},
		"Verify production embedding":                   {"go test -tags=production", "scripts/verify-production-embed.sh"},
	}
	for _, step := range job.Steps {
		for _, command := range expectedRuns[step.Name] {
			if !strings.Contains(step.Run, command) {
				t.Errorf("%s missing expected command %q", step.Name, command)
			}
		}
	}
	if job.Steps[6].WorkingDirectory != "frontend" {
		t.Errorf("frontend build has unexpected working directory %q", job.Steps[6].WorkingDirectory)
	}
	for _, forbidden := range []string{"pull_request_target", "continue-on-error", "secrets.", "|| true"} {
		if strings.Contains(string(b), forbidden) {
			t.Errorf("workflow contains forbidden construct %q", forbidden)
		}
	}
}
