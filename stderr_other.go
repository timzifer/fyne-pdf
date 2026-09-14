//go:build !windows

package pdf

import "github.com/cockroachdb/errors"

// CaptureCStderr ist nur unter Windows implementiert; siehe stderr_windows.go.
func CaptureCStderr(func(line string)) error {
	return errors.New("CaptureCStderr is only implemented on windows")
}
