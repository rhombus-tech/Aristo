// Package chain provides blockchain management for the stateless verification layer
package chain

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
	
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	
	"github.com/rhombus-tech/vm/tee/stateless/core"
)

var (
	// ErrBlockNotFound indicates a block was not found
	ErrBlockNotFound = errors.New("block not found")
	
	// ErrInvalidBlock indicates a block failed validation
	ErrInvalidBlock = errors.New("invalid block")
	
	// ErrInvalidParent indicates a block has an invalid parent
	ErrInvalidParent = errors.New("invalid parent block")
)

// StatelessChainImpl implements the StatelessChain interface
type StatelessChainImpl struct {
	blocks        map[ids.ID]core.StatelessBlock
	heightToID    map[uint64]ids.ID
	latestHeight  uint64
	latestID      ids.ID
	verifier      core.StatelessVerifier
	log           logging.Logger
	
	mutex         sync.RWMutex
}

// NewStatelessChain creates a new stateless blockchain
func NewStatelessChain(verifier core.StatelessVerifier, log logging.Logger) *StatelessChainImpl {
	return &StatelessChainImpl{
		blocks:       make(map[ids.ID]core.StatelessBlock),
		heightToID:   make(map[uint64]ids.ID),
		verifier:     verifier,
		log:          log,
	}
}

// AddBlock adds a new block to the chain
func (c *StatelessChainImpl) AddBlock(ctx context.Context, block core.StatelessBlock) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	
	blockID := block.ID()
	
	// Check if we already have this block
	if _, exists := c.blocks[blockID]; exists {
		return nil // Already have this block
	}
	
	// If not the genesis block, verify parent exists
	if block.Height() > 0 {
		parentID := block.ParentID()
		if _, exists := c.blocks[parentID]; !exists {
			return fmt.Errorf("%w: parent %s not found", ErrInvalidParent, parentID)
		}
	}
	
	// Verify all proofs in the block
	verified, err := block.Verify(ctx, c.verifier)
	if err != nil {
		return fmt.Errorf("failed to verify block: %w", err)
	}
	
	if !verified {
		return ErrInvalidBlock
	}
	
	// Add the block
	c.blocks[blockID] = block
	height := block.Height()
	c.heightToID[height] = blockID
	
	// Update latest if this is a new tip
	if height > c.latestHeight {
		c.latestHeight = height
		c.latestID = blockID
	}
	
	c.log.Info(fmt.Sprintf("Added block to stateless chain: height=%d, blockID=%s, stateRoot=%x", 
		height, 
		blockID, 
		block.StateRoot()))
	
	return nil
}

// GetBlock retrieves a block by its ID
func (c *StatelessChainImpl) GetBlock(ctx context.Context, id ids.ID) (core.StatelessBlock, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	block, exists := c.blocks[id]
	if !exists {
		return nil, ErrBlockNotFound
	}
	
	return block, nil
}

// GetHeight returns the current height of the chain
func (c *StatelessChainImpl) GetHeight(ctx context.Context) (uint64, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	return c.latestHeight, nil
}

// GetLatestStateRoot returns the latest state root
func (c *StatelessChainImpl) GetLatestStateRoot(ctx context.Context) ([sha256.Size]byte, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	if c.latestHeight == 0 && c.latestID == ids.Empty {
		return [sha256.Size]byte{}, nil
	}
	
	latest, exists := c.blocks[c.latestID]
	if !exists {
		return [sha256.Size]byte{}, ErrBlockNotFound
	}
	
	return latest.StateRoot(), nil
}

// VerifyChain verifies the entire chain or a segment of it
func (c *StatelessChainImpl) VerifyChain(ctx context.Context, fromHeight, toHeight uint64) (bool, error) {
	c.mutex.RLock()
	defer c.mutex.RUnlock()
	
	// Validate height range
	if toHeight > c.latestHeight {
		toHeight = c.latestHeight
	}
	
	if fromHeight > toHeight {
		return false, fmt.Errorf("invalid height range: %d > %d", fromHeight, toHeight)
	}
	
	// Collect all blocks in the range
	var blocksToVerify []core.StatelessBlock
	for height := fromHeight; height <= toHeight; height++ {
		id, exists := c.heightToID[height]
		if !exists {
			return false, fmt.Errorf("missing block at height %d", height)
		}
		
		block, exists := c.blocks[id]
		if !exists {
			return false, fmt.Errorf("inconsistent state: block ID exists but block not found at height %d", height)
		}
		
		blocksToVerify = append(blocksToVerify, block)
	}
	
	// Verify all blocks in the range
	for i, block := range blocksToVerify {
		verified, err := block.Verify(ctx, c.verifier)
		if err != nil {
			return false, fmt.Errorf("failed to verify block at height %d: %w", fromHeight+uint64(i), err)
		}
		
		if !verified {
			return false, fmt.Errorf("invalid block at height %d", fromHeight+uint64(i))
		}
		
		// If not the first block, verify parent relationship
		if i > 0 {
			expectedParentID := blocksToVerify[i-1].ID()
			if block.ParentID() != expectedParentID {
				return false, fmt.Errorf("invalid parent link at height %d", fromHeight+uint64(i))
			}
		}
	}
	
	return true, nil
}
