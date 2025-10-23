// Package app utility functions and helpers
package app

import (
	"fmt"
	"os"
)

// parseFileMode parses an octal file mode string into os.FileMode.
//
// This utility function converts string representations of file permissions
// (e.g., "0644", "0755") into Go's os.FileMode type for use with file
// creation and permission setting operations.
//
// Parameters:
//   - modeStr: Octal file mode string (must start with '0' for octal notation)
//
// Returns:
//   - os.FileMode: Parsed file mode for use with os.Chmod and file creation
//   - error: Parse error if the mode string is invalid or malformed
//
// The function expects standard Unix file permission notation:
//   - "0644": Owner read/write, group/other read-only
//   - "0755": Owner read/write/execute, group/other read/execute
//   - "0600": Owner read/write only, no group/other access
//
// This is commonly used for setting permissions on log files, position
// files, and other application-created files.
func parseFileMode(modeStr string) (os.FileMode, error) {
	if len(modeStr) > 0 && modeStr[0] == '0' {
		var mode uint32
		if n, err := fmt.Sscanf(modeStr, "%o", &mode); err == nil && n == 1 {
			return os.FileMode(mode), nil
		}
	}
	return 0, fmt.Errorf("invalid file mode: %s", modeStr)
}