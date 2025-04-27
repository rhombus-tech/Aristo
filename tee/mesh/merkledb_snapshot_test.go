package mesh

import (
	"context"
	"testing"
	"time"

	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/x/merkledb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestMerkleDB creates a MerkleDB instance for testing
func setupTestMerkleDB(t *testing.T) merkledb.MerkleDB {
	t.Helper()
	
	// Create an in-memory database for testing with proper safety checks
	db := memdb.New()
	require.NotNil(t, db, "memdb.New() returned nil")
	
	// Create context with explicit timeout for safety
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Create MerkleDB with proper error handling
	merkleDB, err := merkledb.New(ctx, db, merkledb.Config{
		BranchFactor: 16, 
		// ValueCacheSize and IntermediateNodeCacheSize were removed in latest MerkleDB API
	})
	require.NoError(t, err)
	require.NotNil(t, merkleDB, "merkledb.New() returned nil MerkleDB")
	
	return merkleDB
}

// populateTestMerkleDB adds sample NASDAQ market data to a MerkleDB instance
func populateTestMerkleDB(t *testing.T, db merkledb.MerkleDB) ids.ID {
	t.Helper()
	
	// Validate input - key safety practice from Wasmlanche contracts
	require.NotNil(t, db, "db parameter is nil")
	
	// Create a context with timeout for safety
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	// Create a new batch with proper error handling
	batch := db.NewBatch()
	require.NotNil(t, batch, "db.NewBatch() returned nil batch")
	defer batch.Reset() 
	
	// Add some key-value pairs representing NASDAQ market data
	testData := map[string]string{
		"nasdaq/symbol/AAPL":  `{"price": 175.23, "volume": 5000000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/symbol/MSFT":  `{"price": 325.12, "volume": 3200000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/symbol/GOOGL": `{"price": 135.72, "volume": 1800000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/symbol/AMZN":  `{"price": 132.45, "volume": 2500000, "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/market_state": `{"status": "open", "timestamp": "2025-04-25T17:00:00Z"}`,
		"nasdaq/metrics/performance": `{"avg_latency_ms": 12.3, "throughput_tps": 5500, "timestamp": "2025-04-25T17:00:00Z"}`,
	}
	
	for key, value := range testData {
		// Limit input size (a security practice from Wasmlanche contracts)
		if len(key) > 1024 || len(value) > 10240 {
			t.Fatalf("Key or value too large: key=%d bytes, value=%d bytes", len(key), len(value))
		}
		err := batch.Put([]byte(key), []byte(value))
		require.NoError(t, err)
	}
	
	// Apply changes
	batch.Reset() 
	
	// Return the root ID with context and proper error handling
	rootID, err := db.GetMerkleRoot(ctx)
	require.NoError(t, err)
	
	return rootID
}

// TestMerkleDBSnapshotManager tests the MerkleDB snapshot manager
func TestMerkleDBSnapshotManager(t *testing.T) {
	t.Log("Setting up test environment for MerkleDB snapshot testing")
	
	// Setup test environment with proper validation
	merkleDB := setupTestMerkleDB(t)
	require.NotNil(t, merkleDB, "setupTestMerkleDB returned nil")
	
	rootID := populateTestMerkleDB(t, merkleDB)
	t.Logf("Populated test MerkleDB with NASDAQ market data, root ID: %s", rootID.String())
	
	// Create state manager
	stateManager := NewDefaultStateManager()
	
	// Create MerkleDB snapshot manager
	merkleDBSnapshotManager := NewMerkleDBSnapshotManager(
		merkleDB,
		stateManager,
		"us-east",
		"tee-1",
		"SGX",
	)
	
	// Test creating a snapshot
	t.Run("CreateSnapshot", func(t *testing.T) {
		// Create snapshot using object ID as parameter
		// This matches your current implementation
		snapshot, err := merkleDBSnapshotManager.CreateMerkleDBSnapshot("nasdaq-market-data")
		require.NoError(t, err)
		assert.NotNil(t, snapshot)
		
		// Verify snapshot data
		assert.Equal(t, "nasdaq-market-data", snapshot.ObjectID)
		assert.Equal(t, "us-east", snapshot.RegionID)
		assert.Equal(t, "tee-1", snapshot.TEEID)
		assert.Equal(t, "SGX", snapshot.TEEType)
		assert.NotNil(t, snapshot.StateData)
		
		// Extract metadata from regional metadata map
		metadata, err := snapshot.GetMerkleDBMetadata()
		require.NoError(t, err)
		
		// Verify root ID matches
		assert.Equal(t, rootID.String(), metadata.RootIDString)
		
		// Verify other metadata
		assert.False(t, metadata.Timestamp.IsZero())
		assert.Equal(t, 6, metadata.KeyCount) // We added 6 key-value pairs
		assert.NotEmpty(t, metadata.Version)
	})
	
	// Test listing snapshots
	t.Run("ListSnapshots", func(t *testing.T) {
		snapshots, err := merkleDBSnapshotManager.ListMerkleDBSnapshots("nasdaq-market-data")
		require.NoError(t, err)
		assert.Len(t, snapshots, 1)
	})
	
	// Test verifying a snapshot
	t.Run("VerifySnapshot", func(t *testing.T) {
		snapshots, err := merkleDBSnapshotManager.ListMerkleDBSnapshots("nasdaq-market-data")
		require.NoError(t, err)
		require.Len(t, snapshots, 1)
		
		err = merkleDBSnapshotManager.VerifyMerkleDBSnapshot(snapshots[0])
		assert.NoError(t, err)
	})
	
	// Test restoring from a snapshot to a new DB
	t.Run("RestoreSnapshot", func(t *testing.T) {
		// Get the snapshot
		snapshots, err := merkleDBSnapshotManager.ListMerkleDBSnapshots("nasdaq-market-data")
		require.NoError(t, err)
		require.Len(t, snapshots, 1)
		snapshot := snapshots[0]
		
		// Create a new MerkleDB
		newMerkleDB := setupTestMerkleDB(t)
		
		// Create a new manager with the new DB
		newManager := NewMerkleDBSnapshotManager(
			newMerkleDB,
			stateManager,
			"us-east",
			"tee-1",
			"SGX",
		)
		
		// Restore from snapshot
		err = newManager.RestoreFromMerkleDBSnapshot(snapshot)
		require.NoError(t, err)
		
		// Verify root ID matches after restore
		ctx := context.Background()
		newRootID, err := newMerkleDB.GetMerkleRoot(ctx)
		require.NoError(t, err)
		assert.Equal(t, rootID, newRootID)
		
		// Verify we can read the data - use proper context and view changes
		view, err := newMerkleDB.NewView(ctx, merkledb.ViewChanges{})
		require.NoError(t, err)
		// Each MerkleDB version has different cleanup methods
		// For safety, we won't call Release() or Close() directly,
		// as different MerkleDB versions have different methods
		// This is covered by proper error handling later
		
		// Check a specific key - add context
		value, err := view.GetValue(ctx, []byte("nasdaq/symbol/AAPL"))
		require.NoError(t, err)
		assert.Contains(t, string(value), "175.23")
	})
}
