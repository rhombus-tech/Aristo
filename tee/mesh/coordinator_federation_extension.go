// Package mesh provides a mesh network for TEE-to-TEE communication
package mesh

// GetFederationCoordinator returns the federation coordinator instance.
// This method enables compatibility with the hierarchical federation system.
func (cf *CoordinatorFederation) GetFederationCoordinator() interface{} {
	// Default implementation returns the standard FederationCoordinator
	// In hierarchical federation, this would be overridden to return a HierarchicalFederationCoordinator
	return cf.regionalCoordinator
}
