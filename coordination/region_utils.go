// coordination/region_utils.go
package coordination

import (
	"sort"
)

// getRegionIDs returns a list of all region IDs
func (c *Coordinator) getRegionIDs() []string {
	c.regionLock.RLock()
	defer c.regionLock.RUnlock()

	// Get all region IDs from the teePairs map
	regions := make([]string, 0, len(c.teePairs))
	for regionID := range c.teePairs {
		regions = append(regions, regionID)
	}

	// Sort for deterministic results
	sort.Strings(regions)
	return regions
}

// getRegionMetrics returns metrics for a specific region
func (c *Coordinator) getRegionMetrics(regionID string) *RegionMetrics {
	c.regionLock.RLock()
	defer c.regionLock.RUnlock()

	metrics, ok := c.regionMetrics[regionID]
	if !ok {
		return nil
	}
	return metrics
}

// updateRegionMetrics updates metrics for a specific region
func (c *Coordinator) updateRegionMetrics(regionID string, update func(*RegionMetrics)) {
	c.regionLock.Lock()
	defer c.regionLock.Unlock()

	metrics, ok := c.regionMetrics[regionID]
	if !ok {
		metrics = &RegionMetrics{
			TEEMetrics: make(map[string]*TEEMetrics),
		}
		c.regionMetrics[regionID] = metrics
	}

	// Apply the update
	update(metrics)
}
