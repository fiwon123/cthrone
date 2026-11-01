package exec

import "fmt"

// humanBytes is the size in the unit a person reads.
func humanBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%d B", n)
	}

	value := float64(n)
	units := []string{"KiB", "MiB", "GiB", "TiB"}

	for _, unit := range units {
		value /= 1024

		if value < 1024 {
			return fmt.Sprintf("%.1f %s", value, unit)
		}
	}

	return fmt.Sprintf("%.1f PiB", value/1024)
}
