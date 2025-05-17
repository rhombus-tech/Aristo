package mesh

// Utils file for common utility functions used across the mesh package

// containsString checks if a string is present in a slice of strings
func containsString(s []string, str string) bool {
	for _, v := range s {
		if v == str {
			return true
		}
	}
	return false
}
