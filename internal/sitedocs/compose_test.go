package sitedocs

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Host networking and the data volume are fixed by the image. All other
// documented server settings must reach the binary through each template.
func TestComposeForwardsDocumentedSettings(t *testing.T) {
	guide, err := os.ReadFile("../../docs/self-hosting.md")
	if err != nil {
		t.Fatal(err)
	}
	variables := regexp.MustCompile(`BURZBLOG_[A-Z0-9_]+`)
	settings := map[string]bool{}
	for _, line := range strings.Split(string(guide), "\n") {
		if strings.HasPrefix(line, "|") {
			for _, key := range variables.FindAllString(line, -1) {
				settings[key] = true
			}
		}
	}
	for _, key := range []string{"BURZBLOG_ADDR", "BURZBLOG_DATA", "BURZBLOG_ORIGIN"} {
		delete(settings, key)
	}
	for _, file := range []string{"compose.yaml", "compose.dist.yaml", "deploy/coolify.yaml"} {
		data, err := os.ReadFile("../../" + file)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Services map[string]struct{ Environment yaml.Node }
		}
		if err := yaml.Unmarshal(data, &doc); err != nil {
			t.Fatal(err)
		}
		env := doc.Services["burzblog"].Environment
		values := map[string]string{}
		switch env.Kind {
		case yaml.MappingNode:
			for i := 0; i < len(env.Content); i += 2 {
				values[env.Content[i].Value] = env.Content[i+1].Value
			}
		case yaml.SequenceNode:
			for _, node := range env.Content {
				key, value, _ := strings.Cut(node.Value, "=")
				values[key] = value
			}
		default:
			t.Fatalf("%s has no environment", file)
		}
		for key := range settings {
			if !strings.Contains(values[key], "$"+"{"+key+":-}") {
				t.Errorf("%s does not forward %s", file, key)
			}
		}
	}
}
