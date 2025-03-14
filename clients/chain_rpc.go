package clients

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/noders-team/cosmos-indexer/probe"
	"github.com/noders-team/cosmos-indexer/probe/query"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	types "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/go-resty/resty/v2"
	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/db"
	"github.com/noders-team/cosmos-indexer/rpc"
)

type ChainRPC interface {
	GetBlock(height int64) (*coretypes.ResultBlock, error)
	GetTxsByBlockHeight(height int64) (*txTypes.GetTxsEventResponse, error)
	GetEvmTxsByBlockHeight(height int64, blockTime time.Time) ([]*db.EvmTransaction, error)
	IsCatchingUp() (bool, error)
	GetLatestBlockHeight() (int64, error)
	GetLatestBlockHeightWithRetry(retryMaxAttempts int64, retryMaxWaitSeconds uint64) (int64, error)
	GetEarliestAndLatestBlockHeights() (int64, int64, error)
	TxDecode(ctx context.Context, txBytes *[]byte) (*txTypes.TxDecodeResponse, error)
}

type chainRPC struct {
	cl *probe.ChainClient
}

func NewChainRPC(cl *probe.ChainClient) ChainRPC {
	return &chainRPC{cl: cl}
}

func (c *chainRPC) GetBlock(height int64) (*coretypes.ResultBlock, error) {
	options := query.QueryOptions{Height: height}
	q := query.Query{Client: c.cl, Options: &options}
	resp, err := q.Block()
	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (c *chainRPC) GetTxsByBlockHeight(height int64) (*txTypes.GetTxsEventResponse, error) {
	txs := make([]*txTypes.Tx, 0)
	txsResponses := make([]*types.TxResponse, 0)

	limit := uint64(100)
	pageNum := uint64(1)
	q := query.Query{Client: c.cl, Options: &query.QueryOptions{Height: height}}
	resp, err := q.TxByHeight(c.cl.Codec, pageNum, limit)
	if err != nil {
		return nil, err
	}

	txs = append(txs, resp.Txs...)
	txsResponses = append(txsResponses, resp.TxResponses...)

	if resp.Total > limit {
		totalPages := uint64(math.Ceil(float64(resp.Total-limit) / float64(limit)))

		for totalPages >= pageNum {
			pageNum++

			chunkResp, err := q.TxByHeight(c.cl.Codec, pageNum, limit)
			if err != nil {
				config.Log.Errorf("error getting tx by height %d %s", height, err.Error())
				continue
			}
			txs = append(txs, chunkResp.Txs...)
			txsResponses = append(txsResponses, chunkResp.TxResponses...)
		}
	}

	// TODO unwrap to internal type
	return &txTypes.GetTxsEventResponse{
		Txs:         txs,
		TxResponses: txsResponses,
	}, nil
}

// GetEvmTxsByBlockHeight retrieves EVM transactions for a specific block height from the Berachain EVM RPC endpoint.
// This function requires a properly configured EVM RPC endpoint (cl.EvmRestURl) that supports the Ethereum JSON-RPC API.
// For Berachain, use a reliable RPC endpoint like "https://berachain-testnet-rpc.publicnode.com".
//
// The function performs two RPC calls:
// 1. eth_getBlockByNumber - To get all transactions in the block
// 2. eth_getTransactionReceipt - For each transaction to get its status
//
// Parameters:
//   - height: The block height to retrieve transactions for
//   - blockTime: The timestamp of the block (used for the transaction timestamp)
//
// Returns:
//   - []*db.EvmTransaction: Array of EVM transactions found in the block
//   - error: Any error encountered during the RPC calls or processing
func (c *chainRPC) GetEvmTxsByBlockHeight(height int64, blockTime time.Time) ([]*db.EvmTransaction, error) {
	if c.cl.EvmRestURl == "" {
		return nil, errors.New("EVM URL is not set")
	}

	evmClient := resty.New()

	blockHex := fmt.Sprintf("0x%x", height)
	requestBody := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_getBlockByNumber",
		"params":  []interface{}{blockHex, true},
		"id":      1,
	}

	resp, err := evmClient.R().
		SetHeader("Content-Type", "application/json").
		SetBody(requestBody).
		Post(c.cl.EvmRestURl)
	if err != nil {
		return nil, fmt.Errorf("failed to call EVM RPC: %w", err)
	}

	var result struct {
		Result struct {
			Transactions []map[string]interface{} `json:"transactions"`
			Hash         string                   `json:"hash"`
		} `json:"result"`
	}

	body := resp.Body()
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal EVM RPC response: %s %w", string(body), err)
	}

	// If result.Result is nil, it means the block doesn't exist in the EVM layer
	if result.Result.Hash == "" {
		return []*db.EvmTransaction{}, nil
	}

	blockHash := result.Result.Hash

	evmTxs := make([]*db.EvmTransaction, 0, len(result.Result.Transactions))
	for _, txData := range result.Result.Transactions {
		tx := &db.EvmTransaction{
			Hash:        txData["hash"].(string),
			From:        txData["from"].(string),
			BlockNumber: height,
			BlockHash:   blockHash,
			Timestamp:   blockTime,
		}

		if to, ok := txData["to"].(string); ok && to != "" {
			tx.To = to
		}

		if value, ok := txData["value"].(string); ok {
			decimalValue, err := strconv.ParseInt(value, 0, 64)
			if err == nil {
				tx.Value = fmt.Sprintf("%d", decimalValue)
			} else {
				log.Err(err).Msgf("failed to convert value %s to decimal", value)
			}
		}

		if data, ok := txData["input"].(string); ok {
			dataBytes, _ := hex.DecodeString(strings.TrimPrefix(data, "0x"))
			tx.Data = dataBytes
		}

		if gas, ok := txData["gas"].(string); ok {
			gasInt, _ := strconv.ParseUint(strings.TrimPrefix(gas, "0x"), 16, 64)
			tx.Gas = gasInt
		}

		if gasPrice, ok := txData["gasPrice"].(string); ok {
			decimalValue, err := strconv.ParseInt(gasPrice, 0, 64)
			if err == nil {
				tx.GasPrice = strconv.Itoa(int(decimalValue))
			} else {
				log.Err(err).Msgf("failed to convert value %s to decimal", decimalValue)
			}
		}

		if nonce, ok := txData["nonce"].(string); ok {
			nonceInt, _ := strconv.ParseUint(strings.TrimPrefix(nonce, "0x"), 16, 64)
			tx.Nonce = nonceInt
		}

		if txIndex, ok := txData["transactionIndex"].(string); ok {
			txIndexInt, _ := strconv.ParseUint(strings.TrimPrefix(txIndex, "0x"), 16, 32)
			tx.TxIndex = uint(txIndexInt)
		}

		receiptRequestBody := map[string]interface{}{
			"jsonrpc": "2.0",
			"method":  "eth_getTransactionReceipt",
			"params":  []interface{}{tx.Hash},
			"id":      1,
		}

		receiptResp, err := evmClient.R().
			SetHeader("Content-Type", "application/json").
			SetBody(receiptRequestBody).
			Post(c.cl.EvmRestURl)

		if err == nil {
			var receiptResult struct {
				Result struct {
					Status string `json:"status"`
				} `json:"result"`
			}

			if err := json.Unmarshal(receiptResp.Body(), &receiptResult); err == nil {
				status, err := strconv.ParseUint(strings.TrimPrefix(receiptResult.Result.Status, "0x"), 16, 64)
				if err != nil {
					log.Error().Err(err).Msgf("error parsing status %s", receiptResult.Result.Status)
				}
				tx.Status = status
			}
		}

		evmTxs = append(evmTxs, tx)
	}

	return evmTxs, nil
}

func (c *chainRPC) IsCatchingUp() (bool, error) {
	query := query.Query{Client: c.cl, Options: &query.QueryOptions{}}
	ctx, cancel := query.GetQueryContext()
	defer cancel()

	resStatus, err := query.Client.RPCClient.Status(ctx)
	if err != nil {
		return false, err
	}
	return resStatus.SyncInfo.CatchingUp, nil
}

func (c *chainRPC) GetLatestBlockHeight() (int64, error) {
	q := query.Query{Client: c.cl, Options: &query.QueryOptions{}}
	ctx, cancel := q.GetQueryContext()
	defer cancel()

	resStatus, err := q.Client.RPCClient.Status(ctx)
	if err != nil {
		return 0, err
	}
	return resStatus.SyncInfo.LatestBlockHeight, nil
}

func (c *chainRPC) GetLatestBlockHeightWithRetry(retryMaxAttempts int64, retryMaxWaitSeconds uint64) (int64, error) {
	if retryMaxAttempts == 0 {
		return c.GetLatestBlockHeight()
	}

	if retryMaxWaitSeconds < 2 {
		retryMaxWaitSeconds = 2
	}

	var attempts int64
	maxRetryTime := time.Duration(retryMaxWaitSeconds) * time.Second
	if maxRetryTime < 0 {
		config.Log.Warn("Detected maxRetryTime overflow, setting time to sane maximum of 30s")
		maxRetryTime = 30 * time.Second
	}

	currentBackoffDuration, maxReached := rpc.GetBackoffDurationForAttempts(attempts, maxRetryTime)

	for {
		resp, err := c.GetLatestBlockHeight()
		attempts++
		if err != nil && (retryMaxAttempts < 0 || (attempts <= retryMaxAttempts)) {
			config.Log.Error("Error getting RPC response, backing off and trying again", err)
			config.Log.Debugf("Attempt %d with wait time %+v", attempts, currentBackoffDuration)
			time.Sleep(currentBackoffDuration)

			// guard against overflow
			if !maxReached {
				currentBackoffDuration, maxReached = rpc.GetBackoffDurationForAttempts(attempts, maxRetryTime)
			}

		} else {
			if err != nil {
				config.Log.Error("Error getting RPC response, reached max retry attempts")
			}
			return resp, err
		}
	}
}

func (c *chainRPC) GetEarliestAndLatestBlockHeights() (int64, int64, error) {
	q := query.Query{Client: c.cl, Options: &query.QueryOptions{}}
	ctx, cancel := q.GetQueryContext()
	defer cancel()

	resStatus, err := q.Client.RPCClient.Status(ctx)
	if err != nil {
		return 0, 0, err
	}
	return resStatus.SyncInfo.EarliestBlockHeight, resStatus.SyncInfo.LatestBlockHeight, nil
}

func (c *chainRPC) TxDecode(ctx context.Context, txBytes *[]byte) (*txTypes.TxDecodeResponse, error) {
	q := query.Query{Client: c.cl, Options: &query.QueryOptions{}}
	return q.TxDecode(ctx, txBytes)
}
