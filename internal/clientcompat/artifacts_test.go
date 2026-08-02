package clientcompat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClientExamplesAreParseableTemplatesWithoutEmbeddedCredentials(t *testing.T) {
	exampleDir := filepath.Join("..", "..", "examples", "client-compatibility")
	files := []string{
		"README.md",
		"autogen_chat.py",
		"langchain_chat.py",
		"openai_agents_chat.py",
		"openai_sdk_chat.py",
		"openclaw.provider.json",
		"tencent-workbuddy.models.json",
	}
	for _, name := range files {
		contents, err := os.ReadFile(filepath.Join(exampleDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(contents)
		if strings.Contains(text, "window.location") || strings.Contains(text, "sk-") {
			t.Fatalf("%s contains a browser-derived endpoint or an embedded credential", name)
		}
		if strings.HasSuffix(name, ".json") {
			var value any
			if err := json.Unmarshal(contents, &value); err != nil {
				t.Fatalf("parse %s: %v", name, err)
			}
		}
	}
}

func TestClientExamplesKeepEndpointAndCapabilityClaimsFailClosed(t *testing.T) {
	exampleDir := filepath.Join("..", "..", "examples", "client-compatibility")
	read := func(name string) string {
		t.Helper()
		contents, err := os.ReadFile(filepath.Join(exampleDir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		return string(contents)
	}

	readme := read("README.md")
	for _, required := range []string{
		"UNIROUTER_BASE_URL='https://gateway.example.internal/v1'",
		"default Responses path remains unsupported",
		"isolated adapters",
	} {
		if !strings.Contains(readme, required) {
			t.Fatalf("README missing fail-closed contract %q", required)
		}
	}
	if !strings.Contains(read("openai_agents_chat.py"), "OpenAIChatCompletionsModel") {
		t.Fatal("Agents SDK example must opt into the Chat Completions adapter")
	}
	if !strings.Contains(read("tencent-workbuddy.models.json"), "/v1/chat/completions") {
		t.Fatal("WorkBuddy example must use the full Chat Completions URL")
	}
}
