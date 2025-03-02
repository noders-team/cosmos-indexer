package clients

import (
	"strings"
	"testing"
	"time"

	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/probe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test_GetTxsByBlockHeight tests the GetTxsByBlockHeight function for Celestia.
// This test uses a fixed block height that is known to exist in the Celestia mainnet.
func Test_GetTxsByBlockHeight(t *testing.T) {
	// Skip in short mode as this test can be slow
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	// Setup the chain client with the Celestia RPC endpoint
	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "celestia",
		ChainName:     "celestia",
		ChainID:       "celestia",
		RPC:           "http://celestia-mainnet-consensus.itrocket.net:26657",
	})

	// Use a fixed block height that we know exists
	testHeight := int64(2480035)

	t.Logf("Testing block height: %d", testHeight)

	// Try to get transactions from the block
	rpcClient := NewChainRPC(cl)
	resp, err := rpcClient.GetTxsByBlockHeight(testHeight)

	// If we get an error about the method not being available, skip the test
	if err != nil && strings.Contains(err.Error(), "method") && strings.Contains(err.Error(), "not available") {
		t.Logf("RPC endpoint doesn't support the required method: %v", err)
		t.Skip("Skipping test as the RPC endpoint doesn't support the required method")
		return
	}

	require.NoError(t, err, "Should be able to get transactions from the block")

	// Log the number of transactions found
	t.Logf("Found %d transactions in block %d", len(resp.Txs), testHeight)

	// Verify that the response is properly structured
	require.Equal(t, len(resp.Txs), len(resp.TxResponses), "Number of transactions should match number of responses")

	// If we found transactions, validate the first one
	if len(resp.Txs) > 0 {
		// Validate that the transaction has the expected fields
		require.NotNil(t, resp.Txs[0].Body, "Transaction body should not be nil")
		require.NotNil(t, resp.TxResponses[0], "Transaction response should not be nil")
	}
}

// Test_GetTxsByBlockHeightBera tests the GetTxsByBlockHeight function for Berachain.
// This test uses the latest block height to ensure it works with the current state of the chain.
func Test_GetTxsByBlockHeightBera(t *testing.T) {
	// Setup the chain client with the Berachain RPC endpoint
	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "bera",
		ChainName:     "bera",
		ChainID:       "bera",
		RPC:           "https://berachain-testnet-rpc.publicnode.com",
	})

	// Get the latest block height
	rpcClient := NewChainRPC(cl)
	latestHeight, err := rpcClient.GetLatestBlockHeight()
	if err != nil {
		t.Logf("Could not get latest block height: %v", err)
		t.Skip("Skipping test as we couldn't get the latest block height")
		return
	}

	t.Logf("Latest block height: %d", latestHeight)

	// Try to get transactions from the latest block
	resp, err := rpcClient.GetTxsByBlockHeight(latestHeight)
	require.NoError(t, err, "Should be able to get transactions from the latest block")

	// Log the number of transactions found
	t.Logf("Found %d transactions in block %d", len(resp.Txs), latestHeight)

	// Verify that the response is properly structured
	require.Equal(t, len(resp.Txs), len(resp.TxResponses), "Number of transactions should match number of responses")

	// If we found transactions, validate the first one
	if len(resp.Txs) > 0 {
		// Validate that the transaction has the expected fields
		require.NotNil(t, resp.Txs[0].Body, "Transaction body should not be nil")
		require.NotNil(t, resp.TxResponses[0], "Transaction response should not be nil")
	}
}

// TestChainRPC_GetEvmTxsByBlockHeight tests the GetEvmTxsByBlockHeight function.
// This test uses a fixed block height that is known to exist in the Berachain testnet.
// The test validates the structure and content of the returned EVM transactions.
func TestChainRPC_GetEvmTxsByBlockHeight(t *testing.T) {
	// Skip in short mode
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	// Setup the chain client with the Berachain RPC endpoint
	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "bera",
		ChainName:     "bera",
		ChainID:       "bera",
		RPC:           "https://berachain-testnet-rpc.publicnode.com",
	})

	// Set the EVM REST URL
	cl.EvmRestURl = "https://berachain-testnet-rpc.publicnode.com"

	// Use a fixed block height that we know exists in the EVM layer
	testHeight := int64(10994467)
	blockTime := time.Now() // In a real scenario, you'd get this from the block data

	t.Logf("Testing EVM transactions for block height: %d", testHeight)

	// Create the RPC client and call the function
	rpcClient := NewChainRPC(cl)
	evmTxs, err := rpcClient.GetEvmTxsByBlockHeight(testHeight, blockTime)

	// Validate results
	require.NoError(t, err, "Should not return an error when retrieving EVM transactions")

	// Log the number of transactions found
	t.Logf("Found %d EVM transactions in block %d", len(evmTxs), testHeight)

	require.NotEmpty(t, evmTxs, "Should have at least one EVM transaction")
	// Validate the first transaction
	tx := evmTxs[0]

	// Basic structure validation
	assert.NotEmpty(t, tx.Hash, "Transaction hash should not be empty")
	assert.NotEmpty(t, tx.From, "From address should not be empty")
	assert.Equal(t, testHeight, tx.BlockNumber, "Block number should match the requested height")
	assert.NotEmpty(t, tx.BlockHash, "Block hash should not be empty")

	// Validate transaction fields
	assert.True(t, tx.Gas > 0, "Gas should be greater than 0")
	assert.NotEmpty(t, tx.GasPrice, "Gas price should not be empty")

	// Check if we have transaction receipt data
	if tx.Status == 1 {
		t.Log("Transaction was successful")
	} else if tx.Status == 0 {
		t.Log("Transaction failed")
	}

	// Print some transaction details for manual verification
	t.Logf("Transaction Hash: %s", tx.Hash)
	t.Logf("From: %s", tx.From)
	if tx.To != "" {
		t.Logf("To: %s", tx.To)
	} else {
		t.Log("To: Contract Creation")
	}
	t.Logf("Value: %s", tx.Value)
}
