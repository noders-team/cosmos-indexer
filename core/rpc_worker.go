package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/noders-team/cosmos-indexer/probe"

	cmjson "github.com/cometbft/cometbft/libs/json"

	"github.com/rs/zerolog/log"

	"github.com/noders-team/cosmos-indexer/clients"

	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/noders-team/cosmos-indexer/config"
	dbTypes "github.com/noders-team/cosmos-indexer/db"
	"github.com/noders-team/cosmos-indexer/rpc"
	"gorm.io/gorm"
)

type IndexerBlockEventData struct {
	BlockData                *ctypes.ResultBlock
	BlockResultsData         *ctypes.ResultBlockResults
	BlockEventRequestsFailed bool
	GetTxsResponse           *txTypes.GetTxsEventResponse
	TxRequestsFailed         bool
	IndexTransactions        bool
	EvmTransactions          []*dbTypes.EvmTransaction
}

func (s *IndexerBlockEventData) MarshalJSON(cdc *probe.Codec) ([]byte, error) {
	txResp, err := cdc.Marshaler.Marshal(s.GetTxsResponse)
	if err != nil {
		return nil, err
	}

	blData, err := cmjson.Marshal(s.BlockData)
	if err != nil {
		return nil, err
	}

	blResultsData, err := cmjson.Marshal(s.BlockResultsData)
	if err != nil {
		return nil, err
	}

	data := map[string]interface{}{
		"BlockData":                base64.StdEncoding.EncodeToString(blData),
		"BlockResultsData":         base64.StdEncoding.EncodeToString(blResultsData),
		"BlockEventRequestsFailed": s.BlockEventRequestsFailed,
		"GetTxsResponse":           base64.StdEncoding.EncodeToString(txResp),
		"TxRequestsFailed":         s.TxRequestsFailed,
		"IndexTransactions":        s.IndexTransactions,
		"EvmTransactions":          s.EvmTransactions,
	}

	return json.Marshal(&data)
}

func (s *IndexerBlockEventData) UnmarshalJSON(data []byte) error {
	var mapped map[string]interface{}
	err := json.Unmarshal(data, &mapped)
	if err != nil {
		config.Log.Error("json.Unmarshal(data, &mapped)", err)
		return err
	}

	txResp, found := mapped["GetTxsResponse"]
	if found {
		b, err := base64.StdEncoding.DecodeString(txResp.(string))
		if err != nil {
			return err
		}
		var rxResp txTypes.GetTxsEventResponse
		err = rxResp.Unmarshal(b)
		if err != nil {
			config.Log.Error("json.Unmarshal(GetTxsResponse)", err)
			return err
		}
		s.GetTxsResponse = &rxResp
	}

	var blockData ctypes.ResultBlock
	blDataResp, found := mapped["BlockData"]
	if found {
		b, err := base64.StdEncoding.DecodeString(blDataResp.(string))
		if err != nil {
			return err
		}
		err = cmjson.Unmarshal(b, &blockData)
		if err != nil {
			config.Log.Error("json.Unmarshal(BlockData)", err)
			return err
		}
		s.BlockData = &blockData
	}

	var blockResultsData ctypes.ResultBlockResults
	blResult, found := mapped["BlockResultsData"]
	if found {
		b, err := base64.StdEncoding.DecodeString(blResult.(string))
		if err != nil {
			return err
		}
		err = cmjson.Unmarshal(b, &blockResultsData)
		if err != nil {
			config.Log.Error("json.Unmarshal(BlockResultsData)", err)
			return err
		}
		s.BlockResultsData = &blockResultsData
	}

	blockEventRequestsFailed, _ := mapped["BlockEventRequestsFailed"]
	s.BlockEventRequestsFailed = blockEventRequestsFailed.(bool)

	txRequestsFailed, _ := mapped["TxRequestsFailed"]
	s.TxRequestsFailed = txRequestsFailed.(bool)

	indexTransactions, _ := mapped["IndexTransactions"]
	s.IndexTransactions = indexTransactions.(bool)

	evmTransactions, _ := mapped["EvmTransactions"]
	if evmTransactions != nil {
		evmTransactionList := evmTransactions.([]interface{})
		s.EvmTransactions = make([]*dbTypes.EvmTransaction, len(evmTransactionList))
		for i, evmTransaction := range evmTransactionList {
			transaction := evmTransaction.(map[string]interface{})
			tx := &dbTypes.EvmTransaction{}
			marshaled, err := json.Marshal(transaction)
			if err != nil {
				return err
			}
			err = tx.UnmarshalJSON(marshaled)
			if err != nil {
				return err
			}
			s.EvmTransactions[i] = tx
		}
	}

	return nil
}

type BlockRPCWorker interface {
	Worker(wg *sync.WaitGroup,
		blockEnqueueChan chan *EnqueueData,
		result chan<- IndexerBlockEventData)
	FetchBlock(block *EnqueueData) *IndexerBlockEventData
}

type blockRPCWorker struct {
	chainStringID string
	cfg           *config.IndexConfig
	chainClient   *probe.ChainClient
	db            *gorm.DB
	rpcClient     clients.ChainRPC
}

func NewBlockRPCWorker(
	chainStringID string,
	cfg *config.IndexConfig,
	chainClient *probe.ChainClient,
	db *gorm.DB,
	rpcClient clients.ChainRPC,
) BlockRPCWorker {
	return &blockRPCWorker{
		chainStringID: chainStringID,
		cfg:           cfg,
		chainClient:   chainClient,
		db:            db,
		rpcClient:     rpcClient,
	}
}

// Worker This function is responsible for making all RPC requests to the chain needed for later processing.
// The indexer relies on a number of RPC endpoints for full block data, including block event and transaction searches.
func (w *blockRPCWorker) Worker(wg *sync.WaitGroup,
	blockEnqueueChan chan *EnqueueData,
	result chan<- IndexerBlockEventData,
) {
	defer wg.Done()

	for {
		// Get the next block to process
		block, open := <-blockEnqueueChan
		if !open {
			config.Log.Debugf("Block enqueue channel closed. Exiting RPC worker.")
			break
		}

		tmStart := time.Now()
		log.Debug().Msgf("====> picked ip block for fetching %d %s", block.Height, tmStart.String())
		currentHeightIndexerData := w.FetchBlock(block)
		if currentHeightIndexerData == nil {
			continue
		}

		result <- *currentHeightIndexerData
	}
}

func (w *blockRPCWorker) FetchBlock(block *EnqueueData) *IndexerBlockEventData {
	currentHeightIndexerData := IndexerBlockEventData{
		BlockEventRequestsFailed: false,
		TxRequestsFailed:         false,
		IndexTransactions:        block.IndexTransactions,
	}

	blockData, err := w.rpcClient.GetBlock(block.Height)
	if err != nil {
		// This is the only response we continue on. If we can't get the block, we can't index anything.
		config.Log.Errorf("Error getting block %v from RPC. Err: %v", block, err)
		err := dbTypes.UpsertFailedEventBlock(w.db, block.Height, w.chainStringID, w.cfg.Probe.ChainName)
		if err != nil {
			config.Log.Fatal("Failed to insert failed block event", err)
		}
		err = dbTypes.UpsertFailedBlock(w.db, block.Height, w.chainStringID, w.cfg.Probe.ChainName)
		if err != nil {
			config.Log.Fatal("Failed to insert failed block", err)
		}
		return nil
	}

	currentHeightIndexerData.BlockData = blockData
	if block.IndexTransactions {
		config.Log.Info("Indexing Cosmos transactions")
		err = w.proceedCosmosTx(context.Background(), &currentHeightIndexerData, block.Height)
		if err != nil {
			log.Error().Msgf("Error getting txs for block %v from RPC. Err: %v", block.Height, err)
			return nil
		}
	}

	if block.IndexEVMTransactions {
		config.Log.Info("Indexing EVM transactions")
		err = w.proceedEvmTx(context.Background(), &currentHeightIndexerData, block.Height)
		if err != nil {
			log.Error().Msgf("Error getting txs for block %v from RPC. Err: %v", block.Height, err)
			return nil
		}
	}

	return &currentHeightIndexerData
}

func (w *blockRPCWorker) proceedCosmosTx(ctx context.Context, currentHeightIndexerData *IndexerBlockEventData, blockHeight int64) error {
	txsEventResp, err := w.rpcClient.GetTxsByBlockHeight(blockHeight)

	retryAttempts := int64(5)
	if w.cfg != nil {
		retryAttempts = w.cfg.Base.RequestRetryAttempts
	}

	retryMaxWait := uint64(20)
	if w.cfg != nil {
		retryMaxWait = w.cfg.Base.RequestRetryMaxWait
	}

	if err != nil {
		rpcClient := rpc.URIClient{
			Address: w.chainClient.Config.RPCAddr,
			Client:  &http.Client{},
		}

		if currentHeightIndexerData.BlockResultsData == nil {
			bresults, err := rpc.GetBlockResultWithRetry(rpcClient, blockHeight,
				retryAttempts, retryMaxWait)
			if err != nil {
				config.Log.Errorf("Error getting txs for block %v from RPC. Err: %v", blockHeight, err)
				err := dbTypes.UpsertFailedBlock(w.db, blockHeight, w.chainStringID, w.cfg.Probe.ChainName)
				if err != nil {
					config.Log.Fatal("Failed to insert failed block", err)
				}
				currentHeightIndexerData.GetTxsResponse = nil
				currentHeightIndexerData.BlockResultsData = nil
				currentHeightIndexerData.TxRequestsFailed = true
			} else {
				currentHeightIndexerData.BlockResultsData = bresults
			}
		}
	} else {
		currentHeightIndexerData.GetTxsResponse = txsEventResp
	}

	return nil
}

func (w *blockRPCWorker) proceedEvmTx(ctx context.Context, currentHeightIndexerData *IndexerBlockEventData, blockHeight int64) error {
	txsEventResp, err := w.rpcClient.GetEvmTxsByBlockHeight(blockHeight, currentHeightIndexerData.BlockData.Block.Time)
	if err != nil {
		return err
	}
	log.Debug().Msgf("EVM txs for block %d: %d", blockHeight, len(txsEventResp))
	currentHeightIndexerData.EvmTransactions = txsEventResp

	return nil
}
