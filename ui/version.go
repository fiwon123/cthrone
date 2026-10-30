package ui

// Version is the release this build came from, reported on the About tab.
//
// It is injected the same way the CLI's version is — a -X on the linker line —
// so a GUI user can answer "which build is this" without a terminal, which is
// the first thing a bug report asks for. A build without the injection reports
// vx.x.x, exactly like cmd.Version.
var Version = "vx.x.x"

// placeholderVersion is what an uninjected build reports. Nothing can be
// compared against it, and the About tab says so instead of claiming to be up
// to date with a string that is not a version.
const placeholderVersion = "vx.x.x"
