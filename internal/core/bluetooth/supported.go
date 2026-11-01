//go:build (linux || windows) && !android

package bluetoothcore

// Supported reports that this build carries the Bluetooth transport.
//
// The fact is named here, beside the implementation, instead of being inferred
// at each call site, so the CLI, the unsupported stub and the GUI cannot
// disagree about whether this binary has a radio: one question, one answer. It
// is the rule internal/pairing follows for the credential.
//
// It answers "was the transport compiled in", not "is an adapter plugged in".
// A build that has it can still find no radio, and says so when it looks.
func Supported() bool { return true }
