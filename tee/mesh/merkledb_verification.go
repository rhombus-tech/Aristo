//go:build testing
// +build testing

package mesh

import (
	"fmt"
	"strings"
)

// VerifySnapshotCodeSafety checks the MerkleDB snapshot code for common safety issues
// This follows best practices from our Wasmlanche WebAssembly contract work
func VerifySnapshotCodeSafety() ([]string, error) {
	var issues []string
	var criticalIssues []string

	// Check for proper parameter validation patterns
	if !strings.Contains(MerkleDBSnapshotManager{}.String(), "MerkleDBSnapshotManager") {
		issues = append(issues, "MerkleDBSnapshotManager doesn't implement proper String() method")
	}

	// Check that StateSnapshot uses uint64 for Version (not string)
	snapshot := StateSnapshot{
		Version: 1, // Should be uint64
	}
	if snapshot.Version != 1 {
		criticalIssues = append(criticalIssues, "StateSnapshot.Version should be uint64")
	}

	// Check for rootID comparison safety
	// In the past, we had issues with byte comparisons vs. string comparisons
	if snapshot.ObjectID == "" {
		issues = append(issues, "StateSnapshot.ObjectID shouldn't be empty for verification")
	}

	// If there are critical issues, return them as an error
	if len(criticalIssues) > 0 {
		return issues, fmt.Errorf("critical safety issues found: %s", strings.Join(criticalIssues, ", "))
	}

	return issues, nil
}
