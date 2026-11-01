package update

import (
	"strconv"
	"strings"
)

// parseVersion reads the leading dotted numbers of a version string, and says
// whether there were any.
func parseVersion(version string) ([]int, bool) {
	version = strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(version), "v"), "V")

	numeric := version
	if end := strings.IndexFunc(version, func(r rune) bool {
		return (r < '0' || r > '9') && r != '.'
	}); end >= 0 {
		numeric = version[:end]
	}

	numeric = strings.Trim(numeric, ".")
	if numeric == "" {
		return nil, false
	}

	fields := strings.Split(numeric, ".")
	parts := make([]int, len(fields))
	for i, field := range fields {
		value, err := strconv.Atoi(field)
		if err != nil {
			return nil, false
		}

		parts[i] = value
	}

	return parts, true
}

// compareVersions compares two parsed versions: a missing trailing part is
// zero, so 0.8 and 0.8.0 are the same release.
func compareVersions(a, b []int) int {
	length := len(a)
	if len(b) > length {
		length = len(b)
	}

	for i := 0; i < length; i++ {
		left, right := 0, 0
		if i < len(a) {
			left = a[i]
		}
		if i < len(b) {
			right = b[i]
		}

		switch {
		case left < right:
			return -1
		case left > right:
			return 1
		}
	}

	return 0
}
