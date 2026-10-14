package shared

// Version information. Set at compile-time by `make`/CI (ldflags).
// The default "0.0.0" marks an unversioned local `go build` as a dev
// build, so the in-app updater still offers real releases for it.
var (
	ApplicationVersion string = "0.0.0"
	RevisionDate              = ""
	BuildDate                 = ""
)
