// coordination/utils.go
package coordination

import (
	"fmt"
	"time"
)

// GenerateBatchID generates a unique ID for parameter batches
func GenerateBatchID() string {
	return fmt.Sprintf("batch-%d-%s", time.Now().UnixNano(), randomString(8))
}

// generateLogID creates a unique ID for logging
func generateLogID() string {
	return fmt.Sprintf("log-%d", time.Now().UnixNano())
}
