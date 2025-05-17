package actions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/hypersdk/chain"
	"github.com/ava-labs/hypersdk/codec"
	"github.com/ava-labs/hypersdk/state"
	
	"github.com/rhombus-tech/vm/consts"
	"github.com/rhombus-tech/vm/coordination/xregion"
)

var (
	ErrNilIntent                = errors.New("nil cross-region intent")
	ErrInvalidTimeWindow        = errors.New("invalid time window")
	ErrEmptyStateChanges        = errors.New("empty state changes")
	ErrInvalidStateChanges      = errors.New("invalid state changes")
	ErrInvalidSignature         = errors.New("invalid signature")
	ErrMissingSignature         = errors.New("missing required signature")
	ErrRangeProofTimeout        = errors.New("range proof request timed out")
	ErrRangeProofProcessingFail = errors.New("range proof processing failed")
	ErrTooManyRangeRequests     = errors.New("too many range requests")
	ErrRegionProcessingFailed   = errors.New("region processing failed")
)

// RangeProofJob represents a single range proof request to be processed
type RangeProofJob struct {
	Request   xregion.RangeRequest
	RegionID  string
	Timestamp int64
}

// RangeProofResult contains the result of a range proof request
type RangeProofResult struct {
	Response *xregion.RangeResponse
	RegionID string
	Err      error
}

// RegionProcessingJob represents a single region to be processed
type RegionProcessingJob struct {
	RegionID  string
	IntentID  string
	Signature []byte
}

// RegionProcessingResult contains the result of region processing
type RegionProcessingResult struct {
	RegionID string
	Err      error
}

type CrossRegionAction struct {
	Intent *xregion.CrossRegionIntent `json:"intent"`
}

func (a *CrossRegionAction) Execute(
	ctx context.Context,
	_ chain.Rules,
	mu state.Mutable,
	timestamp int64,
	actor codec.Address,
	_ ids.ID,
) (codec.Typed, error) {
	start := time.Now()
	defer func() {
		// Performance tracking can be added here when metrics package is available
		_ = time.Since(start) // For now, just calculate duration but don't use it
	}()

	// Validate the action
	if err := a.ValidateBasic(); err != nil {
		return nil, err
	}

	coordinator := xregion.GetCoordinator()
	
	// Create RegionProcessor with dynamic concurrency based on CPU count
	cpus := runtime.NumCPU()
	concurrency := cpus
	if cpus > 8 {
		// For machines with many cores, use 75% of cores
		concurrency = (cpus * 3) / 4
	}
	
	processor, err := xregion.NewRegionProcessor(int64(concurrency), coordinator)
	if err != nil {
		return nil, fmt.Errorf("failed to create region processor: %w", err)
	}

	// Process range proofs in parallel
	ranges := a.getRequiredRanges()
	proofs, err := a.processRangeProofsParallel(ctx, coordinator, ranges)
	if err != nil {
		return nil, fmt.Errorf("failed to process range proofs: %w", err)
	}

	// Get list of regions that need confirmation
	regions := a.getRegionsRequiringConfirmation()

	// Process regions in parallel with improved error handling
	signature, err := coordinator.Sign([]byte(a.Intent.ID))
	if err != nil {
		return nil, fmt.Errorf("failed to sign intent: %w", err)
	}

	if err := a.processRegionsParallel(ctx, processor, regions, signature); err != nil {
		return nil, fmt.Errorf("failed to process regions: %w", err)
	}

	// Verify all state changes are covered by proofs
	verificationErrors := a.verifyStateChangesWithProofs(proofs)
	if len(verificationErrors) > 0 {
		// Format useful error message with all verification failures
		errMsg := "state verification failed:\n"
		for regionID, failures := range verificationErrors {
			errMsg += fmt.Sprintf("  Region %s: %d failed verifications\n", regionID, len(failures))
			for i, failure := range failures {
				if i >= 3 { // Limit to first 3 failures per region
					errMsg += fmt.Sprintf("    ... and %d more\n", len(failures)-3)
					break
				}
				errMsg += fmt.Sprintf("    Key: %x\n", failure.Key)
			}
		}
		return nil, errors.New(errMsg)
	}

	// Apply state changes
	for _, changes := range a.Intent.StateChanges {
		if len(changes) == 0 {
			continue
		}
		
		for _, change := range changes {
			key := string(change.Key)
			switch change.Operation {
			case xregion.StateOpSet:
				if err := mu.Insert(ctx, []byte(key), change.Value); err != nil {
					return nil, fmt.Errorf("failed to set state: %w", err)
				}
			case xregion.StateOpTransferOut:
				if err := mu.Remove(ctx, []byte(key)); err != nil {
					return nil, fmt.Errorf("failed to remove state: %w", err)
				}
			}
		}
	}

	return &CrossRegionResult{
		Success: true,
	}, nil
}

func (a *CrossRegionAction) ValidateBasic() error {
	if a.Intent == nil {
		return ErrNilIntent
	}

	// Check time window
	if a.Intent.TimeWindow.Duration <= 0 {
		return ErrInvalidTimeWindow
	}

	// Check state changes
	if len(a.Intent.StateChanges) == 0 {
		return ErrEmptyStateChanges
	}

	// Validate state changes
	if err := a.Intent.ValidateStateChanges(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidStateChanges, err)
	}

	// Verify required signatures
	if err := a.verifySignatures(); err != nil {
		return err
	}

	return nil
}

func (a *CrossRegionAction) verifySignatures() error {
	// Source region must sign
	if _, ok := a.Intent.Signatures[a.Intent.SourceRegion]; !ok {
		return fmt.Errorf("%w: missing source region signature", ErrMissingSignature)
	}

	// All target regions must sign
	for _, region := range a.Intent.TargetRegions {
		if _, ok := a.Intent.Signatures[region]; !ok {
			return fmt.Errorf("%w: missing target region signature", ErrMissingSignature)
		}
	}

	// Verify each signature
	for region, sig := range a.Intent.Signatures {
		if err := xregion.GetCoordinator().VerifySignature(region, a.Intent.ID, sig); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSignature, err)
		}
	}

	return nil
}

func (a *CrossRegionAction) Marshal(p *codec.Packer) error {
	if a.Intent == nil {
		return ErrNilIntent
	}

	// Marshal intent fields
	p.PackString(a.Intent.ID)
	p.PackString(a.Intent.SourceRegion)
	p.PackInt(uint32(len(a.Intent.TargetRegions)))
	for _, region := range a.Intent.TargetRegions {
		p.PackString(region)
	}

	// Marshal timestamps
	p.PackInt64(int64(a.Intent.TimeWindow.Duration))

	// Marshal state changes
	p.PackInt(uint32(len(a.Intent.StateChanges)))
	for region, changes := range a.Intent.StateChanges {
		p.PackString(region)
		p.PackInt(uint32(len(changes)))
		for _, change := range changes {
			p.PackBytes(change.Key)
			p.PackBytes(change.Value)
			p.PackInt(uint32(change.Operation))
			p.PackString(change.Source)
			p.PackString(change.Target)
		}
	}

	// Marshal signatures
	p.PackInt(uint32(len(a.Intent.Signatures)))
	for regionID, sig := range a.Intent.Signatures {
		p.PackString(regionID)
		p.PackBytes(sig)
	}

	return p.Err()
}

func (a *CrossRegionAction) Unmarshal(p *codec.Packer) error {
	a.Intent = &xregion.CrossRegionIntent{
		TargetRegions: make([]string, 0),
		StateChanges:  make(map[string][]xregion.StateChange),
		Signatures:    make(map[string][]byte),
	}

	// Unmarshal intent fields
	a.Intent.ID = p.UnpackString(true)
	a.Intent.SourceRegion = p.UnpackString(true)
	targetCount := p.UnpackInt(true)
	for i := uint32(0); i < targetCount; i++ {
		region := p.UnpackString(true)
		a.Intent.TargetRegions = append(a.Intent.TargetRegions, region)
	}

	// Unmarshal timestamps
	duration := p.UnpackInt64(true)
	a.Intent.TimeWindow.Duration = time.Duration(duration)
	a.Intent.TimeWindow.Start = time.Now() // Set current time as start

	// Unmarshal state changes
	numRegionChanges := p.UnpackInt(true)
	for i := uint32(0); i < numRegionChanges; i++ {
		regionID := p.UnpackString(true)
		numChanges := p.UnpackInt(true)
		changes := make([]xregion.StateChange, numChanges)
		for j := uint32(0); j < numChanges; j++ {
			var key, value []byte
			p.UnpackBytes(0, true, &key)
			p.UnpackBytes(0, true, &value)
			changes[j].Key = key
			changes[j].Value = value
			changes[j].Operation = xregion.StateOperation(p.UnpackInt(true))
			changes[j].Source = p.UnpackString(true)
			changes[j].Target = p.UnpackString(true)
		}
		a.Intent.StateChanges[regionID] = changes
	}

	// Unmarshal signatures
	numSigs := p.UnpackInt(true)
	for i := uint32(0); i < numSigs; i++ {
		regionID := p.UnpackString(true)
		var sig []byte
		p.UnpackBytes(0, true, &sig)
		a.Intent.Signatures[regionID] = sig
	}

	return p.Err()
}

func (a *CrossRegionAction) GetTypeID() uint8 {
	return consts.CrossRegionID
}

func (a *CrossRegionAction) ComputeUnits(r chain.Rules) uint64 {
	// Base cost plus additional cost per state change
	baseCost := uint64(1000)
	stateChangeCost := uint64(100)
	
	totalChanges := uint64(0)
	for _, changes := range a.Intent.StateChanges {
		totalChanges += uint64(len(changes))
	}
	
	return baseCost + (stateChangeCost * totalChanges)
}

func (a *CrossRegionAction) ValidRange(r chain.Rules) (int64, int64) {
	if a.Intent == nil {
		return time.Now().Unix(), time.Now().Add(5 * time.Minute).Unix()
	}
	return a.Intent.TimeWindow.Start.Unix(), a.Intent.TimeWindow.End().Unix()
}

func (a *CrossRegionAction) StateKeys(actor codec.Address) state.Keys {
	keys := make(state.Keys)
	
	// Add all state changes to the keys
	for _, changes := range a.Intent.StateChanges {
		for _, change := range changes {
			key := string(change.Key)
			switch change.Operation {
			case xregion.StateOpSet:
				keys[key] = state.Write
			case xregion.StateOpTransferOut:
				keys[key] = state.Write
			}
		}
	}
	
	return keys
}

// getRequiredRanges determines which ranges need to be requested for state verification
func (a *CrossRegionAction) getRequiredRanges() []xregion.RangeRequest {
	var ranges []xregion.RangeRequest

	// Group changes by region and create range requests
	for regionID, changes := range a.Intent.StateChanges {
		if len(changes) == 0 {
			continue
		}

		// For each region, group changes by key prefix to minimize range requests
		groups := groupChangesByPrefix(changes)

		// Create range request for each group
		for _, group := range groups {
			// Add a small buffer to ensure we get all required keys
			ranges = append(ranges, xregion.RangeRequest{
				RegionID: regionID,
				StartKey: group.Start,
				EndKey:   group.End,
				// Add TimeWindow for time-based verification
				TimeWindow: xregion.TimeWindow{
					Start:    time.Now(),
					Duration: 5 * time.Minute,
				},
			})
		}
	}

	return ranges
}



// getRegionsRequiringConfirmation returns a list of regions that need to be processed
func (a *CrossRegionAction) getRegionsRequiringConfirmation() []string {
	regions := set.Set[string]{}
	
	for regionID, changes := range a.Intent.StateChanges {
		if len(changes) > 0 {
			regions.Add(regionID)
		}
	}
	
	return regions.List()
}

// groupChangesByPrefix groups state changes by their key prefix for efficient range requests
func groupChangesByPrefix(changes []xregion.StateChange) []struct{ Start, End []byte } {
	// If no changes, return empty result
	if len(changes) == 0 {
		return nil
	}
	
	// First sort changes by key
	sort.Slice(changes, func(i, j int) bool {
		return bytes.Compare(changes[i].Key, changes[j].Key) < 0
	})

	// Use adaptive grouping strategy based on distribution
	// Calculate key distribution statistics
	prefixCounts := make(map[string]int)
	for _, change := range changes {
		// Use first 4 bytes as prefix bucket
		prefixLen := 4
		if len(change.Key) < 4 {
			prefixLen = len(change.Key)
		}
		prefix := string(change.Key[:prefixLen])
		prefixCounts[prefix]++
	}

	// Group by common prefix with adaptive strategy
	var groups []struct{ Start, End []byte }
	currentStart := changes[0].Key
	currentEnd := changes[0].Key
	keysInCurrentGroup := 1

	// Target 25-50 keys per range request for optimal performance
	targetGroupSize := calculateOptimalGroupSize(len(changes))

	for i := 1; i < len(changes); i++ {
		// Check if we should start a new group based on:
		// 1. Key proximity
		// 2. Number of keys in current group
		// 3. Prefix distribution
		if !areKeysProximate(currentEnd, changes[i].Key) || keysInCurrentGroup >= targetGroupSize {
			// Add current group
			groups = append(groups, struct{ Start, End []byte }{
				Start: currentStart,
				// End with an extra byte to ensure inclusivity
				End:   makeInclusive(currentEnd),
			})

			// Start new group
			currentStart = changes[i].Key
			keysInCurrentGroup = 1
		} else {
			keysInCurrentGroup++
		}

		currentEnd = changes[i].Key
	}

	// Add final group
	groups = append(groups, struct{ Start, End []byte }{
		Start: currentStart,
		End:   makeInclusive(currentEnd),
	})

	return groups
}

// makeInclusive creates an inclusive end key
func makeInclusive(key []byte) []byte {
	// Append 0xFF to make range inclusive of the end key
	result := make([]byte, len(key)+1)
	copy(result, key)
	result[len(key)] = 0xFF
	return result
}

// calculateOptimalGroupSize determines ideal size for range request groups
func calculateOptimalGroupSize(totalChanges int) int {
	// Use square root scaling for adaptive group sizing
	// This balances between too many small requests vs. few large ones
	sqrt := int(float64(totalChanges) / 2)
	if sqrt < 20 {
		return 20 // Minimum group size
	}
	if sqrt > 100 {
		return 100 // Maximum group size
	}
	return sqrt
}

// areKeysProximate determines if two keys are close enough for efficient range requesting
func areKeysProximate(k1, k2 []byte) bool {
	// Fast path for very similar keys
	prefixLen := commonPrefixLen(k1, k2)
	minLen := min(len(k1), len(k2))
	
	// If keys share a significant prefix, consider them close
	if prefixLen >= minLen/2 {
		return true
	}
	
	// Check if keys are numerically close
	if len(k1) == len(k2) && prefixLen == len(k1)-1 {
		// If keys differ only in the last byte, check numeric proximity
		return abs(int(k1[len(k1)-1]) - int(k2[len(k2)-1])) < 10
	}
	
	return false
}

// commonPrefixLen returns the length of the common prefix of two byte slices
func commonPrefixLen(a, b []byte) int {
	minLen := min(len(a), len(b))
	for i := 0; i < minLen; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return minLen
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// abs returns the absolute value of an integer
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// processRangeProofsParallel processes range proof requests in parallel using a worker pool
func (a *CrossRegionAction) processRangeProofsParallel(ctx context.Context, coordinator *xregion.Coordinator, ranges []xregion.RangeRequest) (map[string]*xregion.RangeResponse, error) {
	if len(ranges) == 0 {
		return make(map[string]*xregion.RangeResponse), nil
	}

	// Create a semaphore to limit concurrent requests
	maxWorkers := runtime.NumCPU() * 2 // Use more workers for IO-bound operations
	if maxWorkers > len(ranges) {
		maxWorkers = len(ranges)
	}

	// Create buffered channels for jobs and results
	jobs := make(chan xregion.RangeRequest, len(ranges))
	results := make(chan struct {
		response *xregion.RangeResponse
		regionID string
		err      error
	}, len(ranges))

	// Create worker pool
	var wg sync.WaitGroup
	// Start workers
	for w := 0; w < maxWorkers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for rng := range jobs {
				// Create context with timeout for each request
				requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
				resp, err := coordinator.RequestRangeProof(requestCtx, &rng)
				cancel()

				// Send result
				results <- struct {
					response *xregion.RangeResponse
					regionID string
					err      error
				}{
					response: resp,
					regionID: rng.RegionID,
					err:      err,
				}
			}
		}()
	}

	// Send jobs to workers
	for _, rng := range ranges {
		jobs <- rng
	}
	close(jobs)

	// Collect results
	proofs := make(map[string]*xregion.RangeResponse)
	errors := make([]error, 0)

	// Use a goroutine to collect results
	go func() {
		wg.Wait()
		close(results)
	}()

	// Process results as they come in
	for result := range results {
		if result.err != nil {
			errors = append(errors, fmt.Errorf("region %s: %w", result.regionID, result.err))
			continue
		}
		proofs[result.regionID] = result.response
	}

	// If any errors occurred, return a combined error
	if len(errors) > 0 {
		errMsg := fmt.Sprintf("%d range proof requests failed:", len(errors))
		for i, err := range errors {
			if i < 3 { // Only show first 3 errors
				errMsg += "\n  " + err.Error()
			} else {
				errMsg += fmt.Sprintf("\n  ... and %d more errors", len(errors)-3)
				break
			}
		}
		return nil, fmt.Errorf(errMsg)
	}

	return proofs, nil
}

// processRegionsParallel processes regions in parallel
func (a *CrossRegionAction) processRegionsParallel(ctx context.Context, processor *xregion.RegionProcessor, regions []string, signature []byte) error {
	if len(regions) == 0 {
		return nil
	}

	// For small number of regions, use direct method
	if len(regions) <= 2 {
		return processor.ProcessRegions(ctx, a.Intent.ID, regions, signature)
	}

	// Create context with timeout
	ctxWithTimeout, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Create error group for parallel processing with cancellation
	g, ctx := sync.WaitGroup{}, ctxWithTimeout
	errorChan := make(chan error, len(regions))

	// Process each region in parallel
	for _, regionID := range regions {
		regionID := regionID // Capture loop variable
		g.Add(1)
		go func() {
			defer g.Done()

			// Process single region individually by simulating the per-region behavior
			// Since ProcessRegion doesn't exist, we'll create an equivalent using ProcessRegions with a single region
			if err := processor.ProcessRegions(ctx, a.Intent.ID, []string{regionID}, signature); err != nil {
				select {
				case errorChan <- fmt.Errorf("failed to process region %s: %w", regionID, err):
				default:
					// Channel full, skip
				}
			}
		}()
	}

	// Wait for all regions to be processed or error
	done := make(chan struct{})
	go func() {
		g.Wait()
		close(done)
	}()

	// Wait for completion or error
	select {
	case <-done:
		// Success, check for any errors
		close(errorChan)
		errorList := make([]error, 0)
		for err := range errorChan {
			errorList = append(errorList, err)
		}

		if len(errorList) > 0 {
			errMsg := fmt.Sprintf("%d regions failed processing:", len(errorList))
			for i, err := range errorList {
				if i < 5 { // Only show first 5 errors
					errMsg += "\n  " + err.Error()
				} else {
					errMsg += fmt.Sprintf("\n  ... and %d more errors", len(errorList)-5)
					break
				}
			}
			return fmt.Errorf(errMsg)
		}
		return nil
	case <-ctx.Done():
		return fmt.Errorf("region processing timed out: %w", ctx.Err())
	}
}

// verifyStateChangesWithProofs verifies that all state changes are covered by range proofs
func (a *CrossRegionAction) verifyStateChangesWithProofs(proofs map[string]*xregion.RangeResponse) map[string][]xregion.StateChange {
	verificationErrors := make(map[string][]xregion.StateChange)

	// For each region's state changes
	for regionID, changes := range a.Intent.StateChanges {
		if len(changes) == 0 {
			continue
		}

		// Check if we have a proof for this region
		proof, exists := proofs[regionID]
		if !exists {
			verificationErrors[regionID] = changes
			continue
		}

		// Optimize verification with a map for O(1) lookup
		entryMap := make(map[string]struct{})
		for key := range proof.Proof.Entries {
			entryMap[key] = struct{}{}
		}

		// Verify each state change
		var failures []xregion.StateChange
		for _, change := range changes {
			// Convert key to string for map lookup
			keyStr := string(change.Key)
			if _, ok := entryMap[keyStr]; !ok {
				failures = append(failures, change)
			}
		}

		if len(failures) > 0 {
			verificationErrors[regionID] = failures
		}
	}

	return verificationErrors
}

type CrossRegionResult struct {
	Success bool `serialize:"true" json:"success"`
}

func (*CrossRegionResult) GetTypeID() uint8 {
	return consts.CrossRegionResultID
}
