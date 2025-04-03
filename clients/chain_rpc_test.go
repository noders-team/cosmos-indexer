package clients

import (
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/db"
	"github.com/noders-team/cosmos-indexer/probe"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_GetTxsByBlockHeight(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "celestia",
		ChainName:     "celestia",
		ChainID:       "celestia",
		RPC:           "http://celestia-mainnet-consensus.itrocket.net:26657",
	})

	testHeight := int64(2480035)

	t.Logf("Testing block height: %d", testHeight)

	rpcClient := NewChainRPC(cl)
	resp, err := rpcClient.GetTxsByBlockHeight(testHeight)

	if err != nil && strings.Contains(err.Error(), "method") && strings.Contains(err.Error(), "not available") {
		t.Logf("RPC endpoint doesn't support the required method: %v", err)
		t.Skip("Skipping test as the RPC endpoint doesn't support the required method")
		return
	}
	require.NoError(t, err, "Should be able to get transactions from the block")
	t.Logf("Found %d transactions in block %d", len(resp.Txs), testHeight)
	require.Equal(t, len(resp.Txs), len(resp.TxResponses), "Number of transactions should match number of responses")

	if len(resp.Txs) > 0 {
		require.NotNil(t, resp.Txs[0].Body, "Transaction body should not be nil")
		require.NotNil(t, resp.TxResponses[0], "Transaction response should not be nil")
	}
}

func Test_GetTxsByBlockHeightBera(t *testing.T) {
	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "bera",
		ChainName:     "bera",
		ChainID:       "bera",
		RPC:           "http://168.119.208.253:26657",
	})

	rpcClient := NewChainRPC(cl)
	latestHeight, err := rpcClient.GetLatestBlockHeight()
	if err != nil {
		t.Logf("Could not get latest block height: %v", err)
		t.Skip("Skipping test as we couldn't get the latest block height")
		return
	}

	t.Logf("Latest block height: %d", latestHeight)

	resp, err := rpcClient.GetTxsByBlockHeight(latestHeight)
	require.NoError(t, err, "Should be able to get transactions from the latest block")

	t.Logf("Found %d transactions in block %d", len(resp.Txs), latestHeight)

	require.Equal(t, len(resp.Txs), len(resp.TxResponses), "Number of transactions should match number of responses")

	if len(resp.Txs) > 0 {
		require.NotNil(t, resp.Txs[0].Body, "Transaction body should not be nil")
		require.NotNil(t, resp.TxResponses[0], "Transaction response should not be nil")
	}
}

func TestChainRPC_GetEvmTxsByBlockHeight(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "bera",
		ChainName:     "bera",
		ChainID:       "bera",
		RPC:           "https://berachain-testnet-rpc.publicnode.com",
	})

	cl.EvmRestURl = "https://berachain-testnet-rpc.publicnode.com"
	//cl.EvmRestURl = "https://berachain-rpc.publicnode.com"

	//testHeight := int64(10994467)
	testHeight := int64(22947)
	blockTime := time.Now()

	t.Logf("Testing EVM transactions for block height: %d", testHeight)

	rpcClient := NewChainRPC(cl)
	evmTxs, err := rpcClient.GetEvmTxsByBlockHeight(testHeight, blockTime)

	require.NoError(t, err, "Should not return an error when retrieving EVM transactions")

	t.Logf("Found %d EVM transactions in block %d", len(evmTxs), testHeight)

	require.NotEmpty(t, evmTxs, "Should have at least one EVM transaction")
	tx := evmTxs[0]

	assert.NotEmpty(t, tx.Hash, "Transaction hash should not be empty")
	assert.NotEmpty(t, tx.From, "From address should not be empty")
	assert.Equal(t, testHeight, tx.BlockNumber, "Block number should match the requested height")
	assert.NotEmpty(t, tx.BlockHash, "Block hash should not be empty")

	assert.True(t, tx.Gas > 0, "Gas should be greater than 0")
	assert.NotEmpty(t, tx.GasPrice, "Gas price should not be empty")

	if tx.Status == 1 {
		t.Log("Transaction was successful")
	} else if tx.Status == 0 {
		t.Log("Transaction failed")
	}

	t.Logf("Transaction Hash: %s", tx.Hash)
	t.Logf("From: %s", tx.From)
	if tx.To != "" {
		t.Logf("To: %s", tx.To)
	} else {
		t.Log("To: Contract Creation")
	}
	t.Logf("Value: %s", tx.Value)
}

func TestParseERC20TransferData(t *testing.T) {
	data := "0xa9059cbb000000000000000000000000f70da97812cb96acdf810712aa562db8dfa3dbef0000000000000000000000000000000000000000000000000000000000f42400"

	hexData := strings.TrimPrefix(data, "0x")
	dataBytes, err := hex.DecodeString(hexData)
	require.NoError(t, err, "Should be able to decode hex data")

	recipient, amount, isERC20 := ParseERC20TransferData(dataBytes)

	require.True(t, isERC20, "Should detect ERC-20 transfer")

	normalizedActual := strings.ToLower(recipient)
	normalizedExpected := strings.ToLower("0xf70da97812cb96acdf810712aa562db8dfa3dbef")

	assert.Equal(t, normalizedExpected, normalizedActual, "Should extract correct recipient address")
	assert.Equal(t, "16000000", amount.String(), "Should extract correct amount")
}

func TestExtractERC20TransferFromLogs(t *testing.T) {
	logs := []interface{}{
		map[string]interface{}{
			"address": "0x1234567890123456789012345678901234567890",
			"topics": []interface{}{
				"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef",
				"0x0000000000000000000000001111111111111111111111111111111111111111",
				"0x0000000000000000000000002222222222222222222222222222222222222222",
			},
			"data": "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000", // 1 token with 18 decimals
		},
	}

	tokenAddr, recipient, amount, found := ExtractERC20TransferFromLogs(logs)

	require.True(t, found, "Should find Transfer event")

	assert.Equal(t,
		strings.ToLower("0x1234567890123456789012345678901234567890"),
		strings.ToLower(tokenAddr),
		"Should extract correct token address")

	assert.Equal(t,
		strings.ToLower("0x2222222222222222222222222222222222222222"),
		strings.ToLower(recipient),
		"Should extract correct recipient")

	assert.Equal(t, "1000000000000000000", amount.String(), "Should extract correct amount")
}

func TestERC20TransactionProcessing(t *testing.T) {
	tx := &db.EvmTransaction{
		Hash:        "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
		From:        "0xabcdef1234567890abcdef1234567890abcdef12",
		To:          "0x1234567890abcdef1234567890abcdef12345678", // Contract address
		Value:       "0",                                          // Zero native value
		BlockNumber: 12345,
		BlockHash:   "0xblockhash",
		Timestamp:   time.Now(),
	}

	inputData := "0xa9059cbb000000000000000000000000f70da97812cb96acdf810712aa562db8dfa3dbef0000000000000000000000000000000000000000000000000000000000f42400"
	hexData := strings.TrimPrefix(inputData, "0x")
	dataBytes, err := hex.DecodeString(hexData)
	require.NoError(t, err, "Should be able to decode input data")

	tx.Data = dataBytes

	recipient, amount, isERC20 := ParseERC20TransferData(dataBytes)

	require.True(t, isERC20, "Should detect an ERC-20 transfer")
	assert.Equal(t, "0xf70da97812cb96acdf810712aa562db8dfa3dbef", strings.ToLower(recipient), "Should extract correct recipient")
	assert.Equal(t, "16000000", amount.String(), "Should extract correct amount")

	logs := []interface{}{
		map[string]interface{}{
			"address": "0x1234567890123456789012345678901234567890", // Token contract
			"topics": []interface{}{
				"0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef", // Transfer
				"0x000000000000000000000000abcdef1234567890abcdef1234567890abcdef12", // From (matches tx.From)
				"0x000000000000000000000000742d35cc6634c0532925a3b844bc454e4438f44e", // To (matches recipient from input data)
			},
			"data": "0x0000000000000000000000000000000000000000000000000de0b6b3a7640000", // Amount (matches amount from input data)
		},
	}

	tokenAddr, logRecipient, logAmount, found := ExtractERC20TransferFromLogs(logs)

	require.True(t, found, "Should find Transfer event in logs")
	assert.Equal(t, "0x1234567890123456789012345678901234567890", tokenAddr, "Should extract correct token contract address")
	assert.Equal(t, "0x742d35cc6634c0532925a3b844bc454e4438f44e", strings.ToLower(logRecipient), "Recipient from logs should match input data")
	assert.Equal(t, "1000000000000000000", logAmount.String(), "Amount from logs should match input data")

	tx.TokenTransfer = &db.EvmTokenTransfer{
		Amount:   amount.String(),
		Receiver: recipient,
		Address:  tokenAddr,
	}

	assert.NotEmpty(t, tx.TokenTransfer.Amount, "Token amount should be set")
	assert.NotEmpty(t, tx.TokenTransfer.Receiver, "Token recipient should be set")
	assert.NotEmpty(t, tx.TokenTransfer.Address, "Token address should be set")
	assert.Equal(t, "0", tx.Value, "Native token value should remain zero")

	t.Logf("Successfully verified ERC-20 token transfer:")
	t.Logf("  Transaction: %s", tx.Hash)
	t.Logf("  Token Contract: %s", tx.TokenTransfer.Address)
	t.Logf("  From: %s", tx.From)
	t.Logf("  To (Recipient): %s", tx.TokenTransfer.Receiver)
	t.Logf("  Amount: %s", tx.TokenTransfer.Amount)
}

func TestChainRPC_HasTokenTransferInTx(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test in short mode")
	}

	cl := probe.GetProbeClient(config.Probe{
		AccountPrefix: "bera",
		ChainName:     "bera",
		ChainID:       "bera",
		RPC:           "https://berachain-testnet-rpc.publicnode.com",
	})

	cl.EvmRestURl = "https://berachain-rpc.publicnode.com"

	testHeight := int64(2658118)
	blockTime := time.Now()

	t.Logf("Testing EVM transactions for block height: %d", testHeight)

	rpcClient := NewChainRPC(cl)
	evmTxs, err := rpcClient.GetEvmTxsByBlockHeight(testHeight, blockTime)

	require.NoError(t, err, "Should not return an error when retrieving EVM transactions")

	t.Logf("Found %d EVM transactions in block %d", len(evmTxs), testHeight)

	require.NotEmpty(t, evmTxs, "Should have at least one EVM transaction")
	foundTx := false
	for _, tx := range evmTxs {
		if tx.Hash == "0x8789c06a8b16db45fd34ebbedf6576e64e03d0794e3ba7368a0bf0f6d28789fd" {
			foundTx = true
			require.NotNil(t, tx.TokenTransfer)
			require.Equal(t, tx.TokenTransfer.Receiver, "0x68a04dbac577d1a9e8442fd368c50d65d304ab17")
			require.Equal(t, tx.TokenTransfer.Amount, "500000000000000000")
			require.Equal(t, tx.TokenTransfer.Address, "0x656b95e550c07a9ffe548bd4085c72418ceb1dba")
		}
	}
	require.True(t, foundTx)
}
