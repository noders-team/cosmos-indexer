package clients

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	types "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/go-resty/resty/v2"
	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/rpc"
	probeClient "github.com/nodersteam/probe/client"
	probeQuery "github.com/nodersteam/probe/query"
)

type ChainRPC interface {
	GetBlock(height int64) (*coretypes.ResultBlock, error)
	GetTxsByBlockHeight(height int64) (*txTypes.GetTxsEventResponse, error)
	GetEvmTxsByBlockHeight(height int64, blockTime time.Time) ([]*EvmTransaction, error)
	IsCatchingUp() (bool, error)
	GetLatestBlockHeight() (int64, error)
	GetLatestBlockHeightWithRetry(retryMaxAttempts int64, retryMaxWaitSeconds uint64) (int64, error)
	GetEarliestAndLatestBlockHeights() (int64, int64, error)
}

type EvmTransaction struct {
	Hash        string
	From        string
	To          string
	Value       string
	Data        []byte
	Gas         uint64
	GasPrice    string
	Nonce       uint64
	BlockHash   string
	BlockNumber int64
	TxIndex     uint
	Status      uint64
	Timestamp   time.Time
}

type chainRPC struct {
	cl  *probeClient.ChainClient
	cfg *config.Config
}

func NewChainRPC(cl *probeClient.ChainClient, cfg *config.Config) ChainRPC {
	return &chainRPC{cl: cl, cfg: cfg}
}

func (c *chainRPC) GetBlock(height int64) (*coretypes.ResultBlock, error) {
	options := probeQuery.QueryOptions{Height: height}
	q := probeQuery.Query{Client: c.cl, Options: &options}
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
	q := probeQuery.Query{Client: c.cl, Options: &probeQuery.QueryOptions{Height: height}}
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

func (c *chainRPC) GetEvmTxsByBlockHeight(height int64, blockTime time.Time) ([]*EvmTransaction, error) {
	if c.cfg.Base.EvmRpcUrl == "" {
		return nil, nil
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
		Post(c.cfg.Base.EvmRpcUrl)

	if err != nil {
		return nil, fmt.Errorf("failed to call EVM RPC: %w", err)
	}

	var result struct {
		Result struct {
			Transactions []map[string]interface{} `json:"transactions"`
			Hash         string                   `json:"hash"`
		} `json:"result"`
	}

	if err := json.Unmarshal(resp.Body(), &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal EVM RPC response: %w", err)
	}

	blockHash := result.Result.Hash

	evmTxs := make([]*EvmTransaction, 0, len(result.Result.Transactions))
	for _, txData := range result.Result.Transactions {
		tx := &EvmTransaction{
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
			tx.Value = value
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
			tx.GasPrice = gasPrice
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
			Post(c.cfg.Base.EvmRpcUrl)

		if err == nil {
			var receiptResult struct {
				Result struct {
					Status string `json:"status"`
				} `json:"result"`
			}

			if err := json.Unmarshal(receiptResp.Body(), &receiptResult); err == nil {
				statusInt, _ := strconv.ParseUint(strings.TrimPrefix(receiptResult.Result.Status, "0x"), 16, 64)
				tx.Status = statusInt
			}
		}

		evmTxs = append(evmTxs, tx)
	}

	return evmTxs, nil
}

func (c *chainRPC) IsCatchingUp() (bool, error) {
	query := probeQuery.Query{Client: c.cl, Options: &probeQuery.QueryOptions{}}
	ctx, cancel := query.GetQueryContext()
	defer cancel()

	resStatus, err := query.Client.RPCClient.Status(ctx)
	if err != nil {
		return false, err
	}
	return resStatus.SyncInfo.CatchingUp, nil
}

func (c *chainRPC) GetLatestBlockHeight() (int64, error) {
	q := probeQuery.Query{Client: c.cl, Options: &probeQuery.QueryOptions{}}
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
	q := probeQuery.Query{Client: c.cl, Options: &probeQuery.QueryOptions{}}
	ctx, cancel := q.GetQueryContext()
	defer cancel()

	resStatus, err := q.Client.RPCClient.Status(ctx)
	if err != nil {
		return 0, 0, err
	}
	return resStatus.SyncInfo.EarliestBlockHeight, resStatus.SyncInfo.LatestBlockHeight, nil
}
