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

	"math/big"

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

// IERC20 transfer method ID (first 4 bytes of keccak256("transfer(address,uint256)"))
const ERC20TransferMethodID = "a9059cbb"

// ERC20 Transfer event signature (keccak256("Transfer(address,address,uint256)"))
const ERC20TransferEventTopic = "0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"

func ParseERC20TransferData(data []byte) (string, *big.Int, bool) {
	if len(data) < 68 {
		log.Debug().Msgf("ERC20: Input data too short (%d bytes) for ERC-20 transfer", len(data))
		return "", nil, false
	}

	methodID := hex.EncodeToString(data[:4])
	if methodID != strings.TrimPrefix(ERC20TransferMethodID, "0x") {
		log.Debug().Msgf("ERC20: Method ID %s is not ERC-20 transfer method (%s)", methodID, ERC20TransferMethodID)
		return "", nil, false
	}

	log.Debug().Msgf("ERC20: Found transfer method signature (%s)", methodID)
	recipientBytes := data[4:36]
	ethAddress := recipientBytes[12:32]
	recipientAddr := "0x" + hex.EncodeToString(ethAddress)
	
	amountBytes := data[36:68]
	amount := big.NewInt(0)
	amount.SetBytes(amountBytes)

	log.Info().
		Str("recipient", recipientAddr).
		Str("amount", amount.String()).
		Msg("ERC20: Successfully parsed transfer data")

	return recipientAddr, amount, true
}

// ExtractERC20TransferFromLogs checks transaction logs for ERC-20 Transfer events
// Returns token contract address, recipient, amount, and whether it found a valid Transfer event
func ExtractERC20TransferFromLogs(logs []interface{}) (string, string, *big.Int, bool) {
	log.Debug().Msgf("ERC20: Scanning %d logs for Transfer events", len(logs))

	for i, logEntry := range logs {
		logMap, ok := logEntry.(map[string]interface{})
		if !ok {
			log.Debug().Msgf("ERC20: Log entry #%d is not a map, skipping", i)
			continue
		}

		topics, ok := logMap["topics"].([]interface{})
		if !ok || len(topics) < 3 {
			log.Debug().Msgf("ERC20: Log entry #%d has invalid topics (need at least 3)", i)
			continue
		}

		eventSig, ok := topics[0].(string)
		if !ok {
			log.Debug().Msgf("ERC20: Log entry #%d has invalid event signature", i)
			continue
		}

		eventSigTrimmed := strings.TrimPrefix(eventSig, "0x")
		if eventSigTrimmed != strings.TrimPrefix(ERC20TransferEventTopic, "0x") {
			log.Debug().Msgf("ERC20: Log entry #%d has non-transfer event signature: %s", i, eventSig)
			continue
		}

		log.Debug().Str("event", eventSig).Msgf("ERC20: Found Transfer event in log #%d", i)

		tokenAddress, ok := logMap["address"].(string)
		if !ok {
			log.Debug().Msgf("ERC20: Missing contract address in log #%d", i)
			continue
		}

		fromTopic, _ := topics[1].(string)
		log.Debug().Str("from_topic", fromTopic).Msg("ERC20: From address in Transfer event")

		toAddrHex, ok := topics[2].(string)
		if !ok {
			log.Debug().Msgf("ERC20: Missing recipient in log #%d", i)
			continue
		}

		toAddrHex = strings.TrimPrefix(toAddrHex, "0x")

		if len(toAddrHex) >= 64 {
			toAddrHex = toAddrHex[len(toAddrHex)-40:]
		} else if len(toAddrHex) < 40 {
			for len(toAddrHex) < 40 {
				toAddrHex = "0" + toAddrHex
			}
		}

		toAddr := "0x" + toAddrHex

		data, ok := logMap["data"].(string)
		if !ok {
			log.Debug().Msgf("ERC20: Missing data field in log #%d", i)
			continue
		}

		dataBytes, err := hex.DecodeString(strings.TrimPrefix(data, "0x"))
		if err != nil || len(dataBytes) < 32 {
			log.Debug().Err(err).Msgf("ERC20: Invalid data in log #%d: %s", i, data)
			continue
		}

		amount := big.NewInt(0)
		amount.SetBytes(dataBytes)

		log.Info().
			Str("token", tokenAddress).
			Str("recipient", toAddr).
			Str("amount", amount.String()).
			Msg("ERC20: Successfully extracted transfer from logs")

		return tokenAddress, toAddr, amount, true
	}

	log.Debug().Msg("ERC20: No valid Transfer events found in logs")
	return "", "", nil, false
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
		log.Error().Msgf("block %d not found in EVM layer", height)
		return []*db.EvmTransaction{}, nil
	}

	blockHash := result.Result.Hash

	evmTxs := make([]*db.EvmTransaction, 0, len(result.Result.Transactions))
	for txIndex, txData := range result.Result.Transactions {
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
			bigValue := big.NewInt(0)
			if _, success := bigValue.SetString(strings.TrimPrefix(value, "0x"), 16); !success {
				log.Err(fmt.Errorf("failed to parse hex value")).
					Msgf("invalid hex value %s", value)
			}
			if bigValue.Sign() < 0 {
				log.Err(fmt.Errorf("negative value, ignoring")).Msgf("invalid hex value %s", value)
				tx.Value = bigValue.String()
			} else {
				tx.Value = big.NewInt(0).String()
			}
		}

		if data, ok := txData["input"].(string); ok {
			dataBytes, _ := hex.DecodeString(strings.TrimPrefix(data, "0x"))
			tx.Data = dataBytes
		}

		if gas, ok := txData["gas"].(string); ok {
			bigValue := big.NewInt(0)
			if _, success := bigValue.SetString(strings.TrimPrefix(gas, "0x"), 16); !success {
				log.Err(fmt.Errorf("failed to parse hex value gas")).
					Msgf("invalid hex value %s", gas)
			}
			tx.Gas = bigValue.Uint64()
		}

		if gasPrice, ok := txData["gasPrice"].(string); ok {
			bigValue := big.NewInt(0)
			if _, success := bigValue.SetString(strings.TrimPrefix(gasPrice, "0x"), 16); !success {
				log.Err(fmt.Errorf("failed to parse hex value gasPrice")).
					Msgf("invalid hex value %s", gasPrice)
			}
			tx.GasPrice = bigValue.String()
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

		if err != nil {
			log.Err(err).Msg("failed to call EVM RPC eth_getTransactionReceipt")
			continue
		}

		var receiptResult struct {
			Result struct {
				Status string        `json:"status"`
				Logs   []interface{} `json:"logs"`
			} `json:"result"`
		}

		if err := json.Unmarshal(receiptResp.Body(), &receiptResult); err == nil {
			status, err := strconv.ParseUint(strings.TrimPrefix(receiptResult.Result.Status, "0x"), 16, 64)
			if err != nil {
				log.Error().Err(err).Msgf("error parsing status %s", receiptResult.Result.Status)
			}
			if status == 1 {
				status = 0
			} else {
				status = 1
			}
			tx.Status = status

			if tx.Value == "0" && tx.TokenTransfer == nil && len(receiptResult.Result.Logs) > 0 {
				log.Debug().
					Str("txHash", tx.Hash).
					Int("logsCount", len(receiptResult.Result.Logs)).
					Msg("Checking transaction logs for ERC-20 transfers")

				tokenAddr, recipient, amount, found := ExtractERC20TransferFromLogs(receiptResult.Result.Logs)
				if found && amount != nil {
					log.Info().
						Str("txHash", tx.Hash).
						Str("tokenContract", tokenAddr).
						Str("recipient", recipient).
						Str("amount", amount.String()).
						Msg("ERC20: Detected transfer from transaction logs")

					tx.TokenTransfer = &db.EvmTokenTransfer{
						Address:  tokenAddr,
						Receiver: recipient,
						Amount:   amount.String(),
					}
				}
			}
		}

		evmTxs = append(evmTxs, tx)
	}

	log.Info().
		Int64("blockHeight", height).
		Int("totalTxs", len(evmTxs)).
		Msg("Finished processing EVM transactions in block")

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
