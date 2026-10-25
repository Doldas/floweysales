package app

// Build metadata, injected at link time:
//
//	go build -ldflags "-X floweysales/internal/app.BuildCommit=<sha> -X floweysales/internal/app.BuildTime=<rfc3339>"
//
// Local and test builds leave them empty, which update checks report as an
// unknown version rather than guessing.
var (
	BuildCommit = ""
	BuildTime   = ""
)
