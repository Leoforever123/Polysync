//go:build !linux && !darwin && !windows

package rootidentity

import "os"

func nativeIdentity(_ *os.File) (identity, error) { return identity{}, nil }
