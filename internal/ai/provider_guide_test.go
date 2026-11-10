package ai

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// guidedDS is a data source whose administrator saved a provider order.
type guidedDS struct {
	fakeDS
	guide string
}

func (g guidedDS) ProviderGuidance(context.Context) string { return g.guide }

func systemOf(t *testing.T, body string) string {
	t.Helper()
	var req struct {
		Messages []struct{ Role, Content string } `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil || len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Fatalf("%v %s", err, body)
	}
	return req.Messages[0].Content
}

func systemPromptFor(t *testing.T, src DataSource, opts AgentOptions) string {
	t.Helper()
	srv, bodies := scriptedOpenAI(t, `{"choices":[{"delta":{"content":"done"},"finish_reason":"stop"}]}`)
	if err := Run(context.Background(), NewClient(srv.URL, "", "test"), src, []Message{{Role: "user", Content: "x"}}, opts, nil); err != nil {
		t.Fatal(err)
	}
	return systemOf(t, (*bodies)[0])
}

func TestTheSavedProviderOrderReachesTheSystemPrompt(t *testing.T) {
	guide := "Provider order an administrator saved: Price history: BBB, then AAA."
	if got := systemPromptFor(t, guidedDS{guide: guide}, AgentOptions{}); !strings.HasSuffix(got, "\n"+guide) {
		t.Errorf("%q", got)
	}
	persona := systemPromptFor(t, guidedDS{guide: guide}, AgentOptions{SystemPrompt: "You are a persona."})
	if !strings.HasPrefix(persona, "You are a persona.") || !strings.HasSuffix(persona, guide) {
		t.Errorf("every agent gets it, after its own prompt: %q", persona)
	}
}

func TestWithoutASavedOrderThePromptIsUnchanged(t *testing.T) {
	if got := systemPromptFor(t, guidedDS{}, AgentOptions{}); got != DefaultSystemPrompt {
		t.Errorf("an empty guide adds nothing")
	}
	if got := systemPromptFor(t, fakeDS{}, AgentOptions{}); got != DefaultSystemPrompt {
		t.Errorf("a source without a guide adds nothing")
	}
}

func TestNoProviderIsNamedInThePromptOrToolDescriptions(t *testing.T) {
	for _, name := range []string{"YAHOO", "BINANCE"} {
		if strings.Contains(DefaultSystemPrompt, name) {
			t.Errorf("the prompt names %s", name)
		}
		for _, tool := range ToolDefs() {
			props, _ := tool.Function.Parameters["properties"].(map[string]any)
			if p, ok := props["provider"].(map[string]any); ok && strings.Contains(p["description"].(string), name) {
				t.Errorf("%s: the provider parameter names %s", tool.Function.Name, name)
			}
		}
	}
}
