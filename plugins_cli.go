package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"floweysales/internal/app"
	"floweysales/internal/plugins"
)

// openPlugins opens the plugin manager from configuration:
//
//	PLUGIN_DIR       installed plugins (default: <user config dir>/floweysales/plugins)
//	PLUGIN_STORE_URL store index (https:// or file://)
//
// Variables a plugin declares (such as FINNHUB_API_KEY) are resolved like
// every other setting: environment first, then config.properties.
func openPlugins(defaults map[string]string) (*plugins.Manager, error) {
	dir := app.ResolveConfig("PLUGIN_DIR", "", defaults)
	if dir == "" {
		base, err := os.UserConfigDir()
		if err != nil {
			return nil, fmt.Errorf("PLUGIN_DIR is not set and there is no user config directory: %w", err)
		}
		dir = filepath.Join(base, "floweysales", "plugins")
	}
	return plugins.Open(plugins.Config{
		Dir:      dir,
		StoreURL: app.ResolveConfig("PLUGIN_STORE_URL", plugins.DefaultStoreURL, defaults),
		LookupEnv: func(name string) string {
			return app.ResolveConfig(name, "", defaults)
		},
	})
}

// runPluginsCommand implements `floweysales_backend plugins …`, the way to
// manage plugins on deployments without authentication (where the Settings
// page refuses) or without a browser. A running backend picks the changes up
// on restart.
func runPluginsCommand(args []string, defaults map[string]string, stdout, stderr io.Writer) int {
	usage := func() {
		fmt.Fprint(stderr, `Usage: floweysales_backend plugins <command>

  list                          installed plugins
  store                         plugins available in the store (checks it now)
  install <id> [version]        install or update from the store
  install --file <path>         install a local plugin binary
  update [id]                   update one or all installed plugins
  uninstall <id>                remove a plugin

  --accept-terms                accept a plugin's terms (install/update)

Restart the backend after changes.
`)
	}
	if len(args) == 0 {
		usage()
		return 2
	}
	// Flags may follow positional arguments ("install avanza --accept-terms").
	command, accept, file := args[0], false, ""
	var rest []string
	for i := 1; i < len(args); i++ {
		switch a := args[i]; {
		case a == "--accept-terms" || a == "-accept-terms":
			accept = true
		case (a == "--file" || a == "-file") && i+1 < len(args):
			i++
			file = args[i]
		case strings.HasPrefix(a, "--file="):
			file = strings.TrimPrefix(a, "--file=")
		case strings.HasPrefix(a, "-"):
			fmt.Fprintf(stderr, "unknown flag %s\n", a)
			usage()
			return 2
		default:
			rest = append(rest, a)
		}
	}

	m, err := openPlugins(defaults)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer m.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	tw := tabwriter.NewWriter(stdout, 0, 4, 2, ' ', 0)
	defer tw.Flush()

	report := func(p plugins.InstalledPlugin, err error) int {
		var terms *plugins.TermsError
		switch {
		case errors.As(err, &terms):
			fmt.Fprintf(stderr, "%s has terms that must be accepted:\n\n%s\n\nRe-run with --accept-terms to accept them.\n", terms.PluginID, terms.Terms)
			return 1
		case err != nil:
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s %s installed (provider %s, sha256 %s)\n", p.Manifest.ID, p.Manifest.Version, p.Manifest.Provider.ID, p.SHA256)
		return 0
	}

	switch command {
	case "list":
		_, _ = m.RefreshStore(ctx) // best effort, for the update column
		ov := m.Overview()
		fmt.Fprintf(tw, "ID\tVERSION\tPROVIDER\tSTATUS\tUPDATE\n")
		for _, p := range ov.Installed {
			status := "ready"
			if !p.Ready {
				status = "needs configuration"
			}
			update := "-"
			if p.UpdateAvailable {
				update = p.LatestVersion
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Manifest.ID, p.Manifest.Version, p.Manifest.Provider.ID, status, update)
		}
		fmt.Fprintf(tw, "\n%d installed in %s\n", len(ov.Installed), m.Dir())
	case "store":
		if _, err := m.RefreshStore(ctx); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(tw, "ID\tLATEST\tPROVIDER\tINSTALLED\tNOTES\n")
		for _, p := range m.Overview().Store.Plugins {
			installed := p.InstalledVersion
			if installed == "" {
				installed = "-"
			}
			notes := p.Reason
			if p.Manifest.Terms != "" {
				notes += " has terms"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.Manifest.ID, p.LatestVersion, p.Manifest.Provider.ID, installed, notes)
		}
	case "install":
		if file != "" {
			return report(m.InstallFile(ctx, file, accept))
		}
		if len(rest) < 1 || len(rest) > 2 {
			usage()
			return 2
		}
		version := ""
		if len(rest) == 2 {
			version = rest[1]
		}
		return report(m.Install(ctx, rest[0], version, accept))
	case "update":
		ids := rest
		if len(ids) == 0 {
			for _, p := range m.Installed() {
				ids = append(ids, p.Manifest.ID)
			}
		}
		code := 0
		for _, id := range ids {
			if c := report(m.Install(ctx, id, "", accept)); c != 0 {
				code = c
			}
		}
		return code
	case "uninstall":
		if len(rest) != 1 {
			usage()
			return 2
		}
		if err := m.Uninstall(rest[0]); err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s uninstalled\n", rest[0])
	default:
		usage()
		return 2
	}
	return 0
}
