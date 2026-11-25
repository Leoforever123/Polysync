//go:build !windows

package rootidentity

import "os"

// The leading dot already hides the marker on these systems.
func hideMarker(*os.File) error { return nil }
