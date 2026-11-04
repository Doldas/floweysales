package server

import "time"

// aboutInfo is what the About page shows about this build. Local and test
// builds have no build data; the page then says "Development build" rather than
// guessing, and an unparseable time is dropped instead of shown.
func aboutInfo(commit, builtAt string) map[string]any {
	info := map[string]any{
		"name":        "FloweySales",
		"license":     "MIT",
		"repository":  "https://github.com/Doldas/floweysales",
		"issues":      "https://github.com/Doldas/floweysales/issues",
		"releases":    "https://github.com/Doldas/floweysales/releases",
		"docs":        "https://doldas.github.io/floweysales/",
		"development": commit == "",
	}
	if commit != "" {
		info["commit"] = commit
	}
	if t, err := time.Parse(time.RFC3339, builtAt); err == nil {
		info["build_time"] = t.UTC().Format(time.RFC3339)
	}
	return info
}
