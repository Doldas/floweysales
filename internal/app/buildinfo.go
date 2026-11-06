package app

// Build metadata, injected at link time:
//
//	go build -ldflags "-X floweysales/internal/app.BuildCommit=<sha> -X floweysales/internal/app.BuildTime=<rfc3339> -X floweysales/internal/app.Version=<semver>"
//
// Local and test builds leave them empty, which update checks report as an
// unknown version rather than guessing.
var (
	BuildCommit = ""
	BuildTime   = ""
	// Version is the release version (for example 0.6.0, or v0.6.0) of a
	// tagged build. Builds that are not releases leave it empty, which plugin
	// version requirements report as unverified rather than guessing.
	Version = ""
)
