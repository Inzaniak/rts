package buildinfo

// These values are replaced by the release workflow through -ldflags.
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)
