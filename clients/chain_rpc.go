package clients

import (
	"context"
	"github.com/noders-team/cosmos-indexer/probe"
	"github.com/noders-team/cosmos-indexer/probe/query"
	"math"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	types "github.com/cosmos/cosmos-sdk/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/rpc"
)

type ChainRPC interface {
	GetBlock(height int64) (*coretypes.ResultBlock, error)
	GetTxsByBlockHeight(height int64) (*txTypes.GetTxsEventResponse, error)
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
