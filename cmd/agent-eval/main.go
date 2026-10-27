// Command agent-eval scores a research model on the FloweySales evaluation
// suite: numeric accuracy, grounding, citations and refusal when data is
// missing. Tool calls are answered from recorded fixtures, so no market
// data provider is contacted; only the model endpoint is.
//
// Usage:
//
//	OPENAI_BASE_URL=http://localhost:11434/v1 OPENAI_MODEL=qwen3:14b \
//	  go run ./cmd/agent-eval -out report.json -min-pass 0.75
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"floweysales/internal/ai"
	"floweysales/internal/eval"
)

func main() {
	var (
		base     = flag.String("base", os.Getenv("OPENAI_BASE_URL"), "OpenAI-compatible endpoint (default $OPENAI_BASE_URL)")
		model    = flag.String("model", os.Getenv("OPENAI_MODEL"), "model name (default $OPENAI_MODEL)")
		cases    = flag.String("cases", "", "dataset JSON (default: the embedded suite)")
		out      = flag.String("out", "", "write the JSON report to this file")
		minPass  = flag.Float64("min-pass", 0, "exit 1 when the pass rate is below this fraction")
		maxCalls = flag.Int("max-tool-calls", 6, "tool-call budget per case")
		timeout  = flag.Duration("timeout", 2*time.Minute, "time limit per case")
	)
	flag.Parse()
	if *base == "" || *model == "" {
		fmt.Fprintln(os.Stderr, "agent-eval: set -base and -model (or OPENAI_BASE_URL and OPENAI_MODEL)")
		os.Exit(2)
	}
	suite, err := loadSuite(*cases)
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-eval:", err)
		os.Exit(2)
	}
	client := ai.NewClient(*base, os.Getenv("OPENAI_API_KEY"), *model)
	report := eval.Run(context.Background(), client, *model, suite, eval.Options{MaxToolCalls: *maxCalls, Timeout: *timeout})

	for _, r := range report.Results {
		status := "PASS"
		if !r.Passed {
			status = "FAIL"
		}
		fmt.Printf("%s %-24s accuracy=%.2f grounded=%t cited=%t tools=%d %dms\n", status, r.ID, r.Accuracy, r.Grounded, r.Cited, r.ToolCalls, r.DurationMS)
		for _, p := range r.Problems {
			fmt.Printf("     - %s\n", p)
		}
		if r.Error != "" {
			fmt.Printf("     - error: %s\n", r.Error)
		}
	}
	s := report.Summary
	fmt.Printf("\n%s on suite %s: %d/%d passed (%.0f%%), accuracy %.2f, grounded %.0f%%, cited %.0f%%, refusals %.0f%%, tool errors %d\n",
		report.Model, report.Suite, s.Passed, s.Cases, 100*s.PassRate, s.Accuracy, 100*s.GroundedRate, 100*s.CitationRate, 100*s.RefusalRate, s.ToolErrors)
	if *out != "" {
		raw, _ := json.MarshalIndent(report, "", "  ")
		if err := os.WriteFile(*out, append(raw, '\n'), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "agent-eval:", err)
			os.Exit(2)
		}
	}
	if s.PassRate < *minPass {
		os.Exit(1)
	}
}

func loadSuite(path string) (eval.Suite, error) {
	if path == "" {
		return eval.DefaultSuite()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return eval.Suite{}, err
	}
	return eval.ParseSuite(raw)
}
