package tx

import (
	"cosmossdk.io/math"
	"encoding/hex"
	"fmt"
	"github.com/noders-team/cosmos-indexer/probe"
	"reflect"
	"strings"
	"time"
	"unsafe"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	"github.com/cosmos/cosmos-sdk/types"
	cosmosTx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/core"
	txtypes "github.com/noders-team/cosmos-indexer/cosmos/modules/tx"
	dbTypes "github.com/noders-team/cosmos-indexer/db"
	"github.com/noders-team/cosmos-indexer/filter"
	"github.com/noders-team/cosmos-indexer/pkg/model"
	"github.com/rs/zerolog/log"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type Parser interface {
	ProcessRPCBlockByHeightTXs(messageTypeFilters []filter.MessageTypeFilter,
		blockResults *coretypes.ResultBlock, resultBlockRes *coretypes.ResultBlockResults) ([]dbTypes.TxDBWrapper, error)
	ProcessRPCTXs(messageTypeFilters []filter.MessageTypeFilter,
		txEventResp *cosmosTx.GetTxsEventResponse) ([]dbTypes.TxDBWrapper, error)
	ProcessEvmTxs(data *core.IndexerBlockEventData) ([]dbTypes.TxDBWrapper, error)
}

type parser struct {
	db        *gorm.DB
	cl        *probe.ChainClient
	processor Processor
}

func NewParser(db *gorm.DB, cl *probe.ChainClient, processor Processor) Parser {
	return &parser{db: db, cl: cl, processor: processor}
}

func (a *parser) ProcessRPCBlockByHeightTXs(messageTypeFilters []filter.MessageTypeFilter,
	blockResults *coretypes.ResultBlock, resultBlockRes *coretypes.ResultBlockResults,
) ([]dbTypes.TxDBWrapper, error) {
	if len(blockResults.Block.Txs) != len(resultBlockRes.TxResults) {
		config.Log.Fatalf("blockResults & resultBlockRes: different length %d != %d", len(blockResults.Block.Txs), len(resultBlockRes.TxResults))
	}

	blockTime := &blockResults.Block.Time
	blockTimeStr := blockTime.Format(time.RFC3339)
	currTxDbWrappers := make([]dbTypes.TxDBWrapper, len(blockResults.Block.Txs))

	for txIdx, tendermintTx := range blockResults.Block.Txs {
		txResult := resultBlockRes.TxResults[txIdx]

		// Indexer types only used by the indexer app (similar to the cosmos types)
		var indexerMergedTx txtypes.MergedTx
		var indexerTx txtypes.IndexerTx
		var txBody txtypes.Body
		var currMessages []types.Msg
		var currLogMsgs []txtypes.LogMessage
		var err error

		txFull, err := core.Decode(a.cl.Codec, []byte(tendermintTx.String()))
		if err != nil {
			log.Info().Msgf("error decoding transaction from: %s", tendermintTx.String())
			return nil, err
		}

		/*txDecoder := a.cl.Codec.TxConfig.TxDecoder()

		txBasic, err := txDecoder(tendermintTx)
		var txFull cosmosTx.Tx
		if err != nil {
			txBasic, err = core.InAppTxDecoder(a.cl.Codec)(tendermintTx)
			if err != nil {
				return nil, blockTime, fmt.Errorf("ProcessRPCBlockByHeightTXs: TX cannot be parsed from block %v. This is usually a proto definition error. Err: %v", blockResults.Block.Height, err)
			}
			txFull = txBasic
		} else {
			// This is a hack, but as far as I can tell necessary. "wrapper" struct is private in Cosmos SDK.
			field := reflect.ValueOf(txBasic).Elem().FieldByName("tx")
			iTx := a.getUnexportedField(field)
			txFull = iTx.(cosmosTx.Tx)
		}*/

		logs := types.ABCIMessageLogs{}
		// Failed TXs do not have proper JSON in the .Log field, causing ParseABCILogs to fail to unmarshal the logs
		// We can entirely ignore failed TXs in downstream parsers, because according to the Cosmos specification, a single failed message in a TX fails the whole TX
		if txResult.Code == 0 && strings.Trim(txResult.Log, "") != "" {
			logs, err = types.ParseABCILogs(txResult.Log)
			if err != nil {
				log.Warn().Msgf("logs cannot be parsed  using events instead %+v, %s", txResult, err.Error())
			}
		}

		txHash := tendermintTx.Hash()

		var messagesRaw [][]byte

		events := make(types.StringEvents, 0)
		if len(logs) == 0 {
			for _, event := range txResult.Events {
				attr := make([]types.Attribute, 0)
				for _, attribute := range event.Attributes {
					attr = append(attr, types.Attribute{
						Key:   attribute.Key,
						Value: attribute.Value,
					})
				}
				events = append(events, types.StringEvent{
					Type:       event.Type,
					Attributes: attr,
				})
			}
		}

		// Get the Messages and Message Logs
		for msgIdx := range txFull.Body.Messages {
			shouldIndex, err := a.messageTypeShouldIndex(txFull.Body.Messages[msgIdx].TypeUrl, messageTypeFilters)
			if err != nil {
				config.Log.Error("messageTypeShouldIndex", err)
				return nil, err
			}

			if !shouldIndex {
				config.Log.Debug(fmt.Sprintf("[Block: %v] [TX: %v] Skipping msg of type '%v'.",
					blockResults.Block.Height, tendermintHashToHex(txHash), txFull.Body.Messages[msgIdx].TypeUrl))
				currMessages = append(currMessages, nil)
				currLogMsgs = append(currLogMsgs, txtypes.LogMessage{
					MessageIndex: msgIdx,
				})
				messagesRaw = append(messagesRaw, nil)
				continue
			}

			currMsg := txFull.Body.Messages[msgIdx].GetCachedValue()

			if currMsg != nil {
				msg := currMsg.(types.Msg)
				messagesRaw = append(messagesRaw, txFull.Body.Messages[msgIdx].Value)
				currMessages = append(currMessages, msg)
				msgEvents := types.StringEvents{}
				if txResult.Code == 0 {
					if len(logs) > 0 {
						msgEvents = logs[msgIdx].Events
					} else {
						msgEvents = events
					}
				}

				currTxLog := txtypes.LogMessage{
					MessageIndex: msgIdx,
					Events:       a.toEvents(msgEvents),
				}
				currLogMsgs = append(currLogMsgs, currTxLog)
			}
		}

		txBody.Messages = currMessages
		indexerTx.Body = txBody

		indexerTxResp := txtypes.Response{
			TxHash:    tendermintHashToHex(txHash),
			Height:    fmt.Sprintf("%d", blockResults.Block.Height),
			TimeStamp: blockTimeStr,
			RawLog:    []byte(txResult.Log),
			Log:       currLogMsgs,
			Code:      txResult.Code,
			GasUsed:   txResult.GasUsed,
			GasWanted: txResult.GasWanted,
			Codespace: txResult.Codespace,
			Info:      txResult.Info,
			// Data:      string(txResult.Data), TODO
		}

		indexerTx.AuthInfo = *txFull.AuthInfo
		indexerMergedTx.TxResponse = indexerTxResp
		indexerMergedTx.Tx = indexerTx
		indexerMergedTx.Tx.AuthInfo = *txFull.AuthInfo

		processedTx, _, err := a.processor.ProcessTx(indexerMergedTx, messagesRaw)
		if err != nil {
			config.Log.Error("ProcessTx", err)
			return currTxDbWrappers, err
		}

		filteredSigners := []types.AccAddress{}
		/* TODO, not in new cosmos SDK
		for _, filteredMessage := range txBody.Messages {
			if filteredMessage != nil {
				err := filteredMessage.ValidateBasic()
				if err != nil {
					config.Log.Error("error validating filtered message, ignoring", err)
					continue
				}
				filteredSigners = append(filteredSigners, filteredMessage.GetSigners()...)
			}
		}*/

		signers, signerInfos, err := a.processor.ProcessSigners(txFull.AuthInfo, filteredSigners)
		if err != nil {
			config.Log.Error("ProcessSigners", err)
			return currTxDbWrappers, err
		}

		processedTx.Tx.SignerAddresses = signers

		fees, err := a.processor.ProcessFees(indexerTx.AuthInfo, signers)
		if err != nil {
			config.Log.Error("ProcessFees", err)
			return currTxDbWrappers, err
		}

		processedTx.Tx.Fees = fees

		// extra fields
		processedTx.Tx.Signatures = txFull.Signatures
		processedTx.Tx.Memo = txFull.Body.Memo
		processedTx.Tx.TimeoutHeight = txFull.Body.TimeoutHeight

		extensionOptions := make([]string, 0)
		for _, opt := range txFull.Body.ExtensionOptions {
			extensionOptions = append(extensionOptions, opt.String())
		}
		processedTx.Tx.ExtensionOptions = extensionOptions

		nonExtensionOptions := make([]string, 0)
		for _, opt := range txFull.Body.NonCriticalExtensionOptions {
			extensionOptions = append(extensionOptions, opt.String())
		}
		processedTx.Tx.NonCriticalExtensionOptions = nonExtensionOptions
		processedTx.Tx.TxResponse = model.TxResponse{
			TxHash:    indexerTxResp.TxHash,
			Height:    indexerTxResp.Height,
			TimeStamp: indexerTxResp.TimeStamp,
			Code:      indexerTxResp.Code,
			RawLog:    indexerTxResp.RawLog,
			GasUsed:   indexerTxResp.GasUsed,
			GasWanted: indexerTxResp.GasWanted,
			Codespace: indexerTxResp.Codespace,
			Data:      indexerTxResp.Data,
			Info:      indexerTxResp.Info,
		}

		if txFull.AuthInfo != nil && txFull.AuthInfo.Fee != nil {
			txAuthInfo := model.AuthInfo{
				Fee: model.AuthInfoFee{
					Granter:  txFull.AuthInfo.Fee.Granter,
					Payer:    txFull.AuthInfo.Fee.Payer,
					GasLimit: txFull.AuthInfo.Fee.GasLimit,
				},
				SignerInfos: signerInfos,
			}
			if txFull.AuthInfo.Tip != nil {
				tipAmount := make([]model.TipAmount, 0)
				for _, a := range txFull.AuthInfo.Tip.Amount {
					tipAmount = append(tipAmount, model.TipAmount{
						Denom:  a.Denom,
						Amount: decimal.NewFromInt(a.Amount.Int64()),
					})
				}
				txAuthInfo.Tip = model.Tip{
					Tipper: txFull.AuthInfo.Tip.Tipper,
					Amount: tipAmount,
				}
			}

			processedTx.Tx.AuthInfo = txAuthInfo
		}

		currTxDbWrappers[txIdx] = processedTx
	}

	return currTxDbWrappers, nil
}

// ProcessRPCTXs - Given an RPC response, build out the more specific data used by the parser.
func (a *parser) ProcessRPCTXs(messageTypeFilters []filter.MessageTypeFilter,
	txEventResp *cosmosTx.GetTxsEventResponse,
) ([]dbTypes.TxDBWrapper, error) {
	currTxDbWrappers := make([]dbTypes.TxDBWrapper, len(txEventResp.Txs))

	for txIdx := range txEventResp.Txs {
		// Indexer types only used by the indexer app (similar to the cosmos types)
		var indexerMergedTx txtypes.MergedTx
		var indexerTx txtypes.IndexerTx
		var txBody txtypes.Body
		var currMessages []types.Msg
		var currLogMsgs []txtypes.LogMessage
		var messagesRaw [][]byte

		currTx := txEventResp.Txs[txIdx]
		currTxResp := txEventResp.TxResponses[txIdx]

		// Get the Messages and Message Logs
		for msgIdx := range currTx.Body.Messages {

			shouldIndex, err := a.messageTypeShouldIndex(currTx.Body.Messages[msgIdx].TypeUrl, messageTypeFilters)
			if err != nil {
				return nil, err
			}

			if !shouldIndex {
				config.Log.Debug(fmt.Sprintf("[Block: %v] [TX: %v] Skipping msg of type '%v'.", currTxResp.Height, currTxResp.TxHash, currTx.Body.Messages[msgIdx].TypeUrl))
				currMessages = append(currMessages, nil)
				currLogMsgs = append(currLogMsgs, txtypes.LogMessage{
					MessageIndex: msgIdx,
				})
				messagesRaw = append(messagesRaw, nil)
				continue
			}

			currMsg := currTx.Body.Messages[msgIdx].GetCachedValue()
			messagesRaw = append(messagesRaw, currTx.Body.Messages[msgIdx].Value)

			// If we reached here, unpacking the entire TX raw was not successful
			// Attempt to unpack the message individually.
			if currMsg == nil {
				var currMsgUnpack types.Msg

				err = a.cl.Codec.InterfaceRegistry.UnpackAny(currTx.Body.Messages[msgIdx], &currMsgUnpack)
				if err != nil || currMsgUnpack == nil {
					config.Log.Errorf(fmt.Sprintf("tx message could not be processed. "+
						"Unpacking protos failed and CachedValue is not present. "+
						"TX Hash: %s, Msg type: %s, Msg index: %d, Code: %d, Error: %s. Ignoring....",
						currTxResp.TxHash,
						currTx.Body.Messages[msgIdx].TypeUrl,
						msgIdx,
						currTxResp.Code,
						err.Error(),
					))
					continue
					/*
						return nil, blockTime, fmt.Errorf("tx message could not be processed. Unpacking protos failed and CachedValue is not present. TX Hash: %s, Msg type: %s, Msg index: %d, Code: %d",
							currTxResp.TxHash,
							currTx.Body.Messages[msgIdx].TypeUrl,
							msgIdx,
							currTxResp.Code,
						)*/
				}
				currMsg = currMsgUnpack
			}

			if currMsg != nil {
				msg := currMsg.(types.Msg)
				currMessages = append(currMessages, msg)
				if len(currTxResp.Logs) >= msgIdx+1 {
					msgEvents := currTxResp.Logs[msgIdx].Events
					currTxLog := txtypes.LogMessage{
						MessageIndex: msgIdx,
						Events:       a.toEvents(msgEvents),
					}
					currLogMsgs = append(currLogMsgs, currTxLog)
				}
			}
		}

		txBody.Messages = currMessages
		indexerTx.Body = txBody

		indexerTxResp := txtypes.Response{
			TxHash:    currTxResp.TxHash,
			Height:    fmt.Sprintf("%d", currTxResp.Height),
			TimeStamp: currTxResp.Timestamp,
			RawLog:    []byte(currTxResp.RawLog),
			Log:       currLogMsgs,
			Code:      currTxResp.Code,
			GasUsed:   currTxResp.GasUsed,
			GasWanted: currTxResp.GasWanted,
			Info:      currTxResp.Info,
			Data:      currTxResp.Data,
		}

		indexerTx.AuthInfo = *currTx.AuthInfo
		indexerMergedTx.TxResponse = indexerTxResp
		indexerMergedTx.Tx = indexerTx
		indexerMergedTx.Tx.AuthInfo = *currTx.AuthInfo

		processedTx, _, err := a.processor.ProcessTx(indexerMergedTx, messagesRaw)
		if err != nil {
			return currTxDbWrappers, err
		}

		filteredSigners := make([]types.AccAddress, 0)
		/*
			for _, filteredMessage := range txBody.Messages {
				if filteredMessage != nil {
					filteredSigners = append(filteredSigners, filteredMessage.GetSigners()...)
				}
			}*/

		err = currTx.AuthInfo.UnpackInterfaces(a.cl.Codec.InterfaceRegistry)
		if err != nil {
			return currTxDbWrappers, err
		}

		signers, signerInfos, err := a.processor.ProcessSigners(currTx.AuthInfo, filteredSigners)
		if err != nil {
			return currTxDbWrappers, err
		}
		processedTx.Tx.SignerAddresses = signers

		fees, err := a.processor.ProcessFees(indexerTx.AuthInfo, signers)
		if err != nil {
			return currTxDbWrappers, err
		}

		processedTx.Tx.Fees = fees

		// extra fields
		processedTx.Tx.Signatures = currTx.Signatures
		processedTx.Tx.Memo = currTx.Body.Memo
		processedTx.Tx.TimeoutHeight = currTx.Body.TimeoutHeight

		extensionOptions := make([]string, 0)
		for _, opt := range currTx.Body.ExtensionOptions {
			extensionOptions = append(extensionOptions, opt.String())
		}
		processedTx.Tx.ExtensionOptions = extensionOptions

		nonExtensionOptions := make([]string, 0)
		for _, opt := range currTx.Body.NonCriticalExtensionOptions {
			extensionOptions = append(extensionOptions, opt.String())
		}
		processedTx.Tx.NonCriticalExtensionOptions = nonExtensionOptions
		processedTx.Tx.TxResponse = model.TxResponse{
			TxHash:    indexerTxResp.TxHash,
			Height:    indexerTxResp.Height,
			TimeStamp: indexerTxResp.TimeStamp,
			Code:      indexerTxResp.Code,
			RawLog:    indexerTxResp.RawLog,
			GasUsed:   indexerTxResp.GasUsed,
			GasWanted: indexerTxResp.GasWanted,
			Codespace: indexerTxResp.Codespace,
		}

		if currTx.AuthInfo != nil {
			txAuthInfo := model.AuthInfo{
				Fee: model.AuthInfoFee{
					Granter:  currTx.AuthInfo.Fee.Granter,
					Payer:    currTx.AuthInfo.Fee.Payer,
					GasLimit: currTx.AuthInfo.Fee.GasLimit,
				},
				SignerInfos: signerInfos,
			}
			if currTx.AuthInfo.Tip != nil {
				tipAmount := make([]model.TipAmount, 0)
				for _, a := range currTx.AuthInfo.Tip.Amount {
					tipAmount = append(tipAmount, model.TipAmount{
						Denom:  a.Denom,
						Amount: decimal.NewFromInt(a.Amount.Int64()),
					})
				}
				txAuthInfo.Tip = model.Tip{
					Tipper: currTx.AuthInfo.Tip.Tipper,
					Amount: tipAmount,
				}
			}

			processedTx.Tx.AuthInfo = txAuthInfo
		}

		currTxDbWrappers[txIdx] = processedTx
	}

	return currTxDbWrappers, nil
}

func (a *parser) messageTypeShouldIndex(messageType string, filters []filter.MessageTypeFilter) (bool, error) {
	if len(filters) != 0 {
		filterData := filter.MessageTypeData{
			MessageType: messageType,
		}

		matches := false
		for _, messageTypeFilter := range filters {
			typeMatch, err := messageTypeFilter.MessageTypeMatches(filterData)
			if err != nil {
				return false, err
			}
			if typeMatch {
				matches = true
				break
			}
		}

		return matches, nil
	}

	return true, nil
}

func (a *parser) toAttributes(attrs []types.Attribute) []txtypes.Attribute {
	list := []txtypes.Attribute{}
	for _, attr := range attrs {
		lma := txtypes.Attribute{Key: attr.Key, Value: attr.Value}
		list = append(list, lma)
	}

	return list
}

func (a *parser) toEvents(msgEvents types.StringEvents) (list []txtypes.LogMessageEvent) {
	for _, evt := range msgEvents {
		lme := txtypes.LogMessageEvent{Type: evt.Type, Attributes: a.toAttributes(evt.Attributes)}
		list = append(list, lme)
	}

	return list
}

func (a *parser) getUnexportedField(field reflect.Value) interface{} {
	return reflect.NewAt(field.Type(), unsafe.Pointer(field.UnsafeAddr())).Elem().Interface()
}

func tendermintHashToHex(hash []byte) string {
	return strings.ToUpper(hex.EncodeToString(hash))
}

// Unmarshal JSON to a particular type. There can be more than one handler for each type.
// TODO: Remove this map and replace with a more generic solution
var messageTypeHandler = map[string][]func() txtypes.CosmosMessage{}

// var messageTypeIgnorer = map[string]interface{}{}

// Merge the chain specific message type handlers into the core message type handler map.
// Chain specific handlers will be registered BEFORE any generic handlers.
// TODO: Remove this function and replace with a more generic solution
func ChainSpecificMessageTypeHandlerBootstrap(chainID string) {
	var chainSpecificMessageTpeHandler map[string][]func() txtypes.CosmosMessage
	for key, value := range chainSpecificMessageTpeHandler {
		if list, ok := messageTypeHandler[key]; ok {
			messageTypeHandler[key] = append(value, list...)
		} else {
			messageTypeHandler[key] = value
		}
	}
}

func (a *parser) ProcessEvmTxs(data *core.IndexerBlockEventData) ([]dbTypes.TxDBWrapper, error) {
	if len(data.EvmTransactions) == 0 {
		return nil, nil
	}
	currTxDbWrappers := make([]dbTypes.TxDBWrapper, len(data.EvmTransactions))

	for txIdx := range data.EvmTransactions {
		var indexerMergedTx txtypes.MergedTx
		var indexerTx txtypes.IndexerTx
		var txBody txtypes.Body
		var currLogMsgs []txtypes.LogMessage
		var messagesRaw [][]byte

		txBody.Messages = make([]types.Msg, 0)
		indexerTx.Body = txBody

		currTxResp := data.EvmTransactions[txIdx]
		indexerTxResp := txtypes.Response{
			TxHash:    currTxResp.Hash,
			Height:    fmt.Sprintf("%d", currTxResp.BlockNumber),
			TimeStamp: currTxResp.Timestamp.String(),
			RawLog:    currTxResp.Data,
			Log:       currLogMsgs,
			Code:      1,
			GasUsed:   int64(currTxResp.Gas),
			GasWanted: int64(currTxResp.Gas),
			Info:      "",
			Data:      string(currTxResp.Data),
		}

		authInfo := cosmosTx.AuthInfo{
			Fee: &cosmosTx.Fee{
				Amount: []types.Coin{
					{
						Denom:  "eth", // TODO
						Amount: math.NewInt(int64(currTxResp.Gas)),
					},
				},
				GasLimit: currTxResp.Gas, // TODO
			},
			SignerInfos: make([]*cosmosTx.SignerInfo, 0),
		}

		indexerTx.AuthInfo = authInfo
		indexerMergedTx.TxResponse = indexerTxResp
		indexerMergedTx.Tx = indexerTx
		indexerMergedTx.Tx.AuthInfo = authInfo

		processedTx, _, err := a.processor.ProcessTx(indexerMergedTx, messagesRaw)
		if err != nil {
			return currTxDbWrappers, err
		}

		filteredSigners := make([]types.AccAddress, 0)

		signers, signerInfos, err := a.processor.ProcessSigners(&authInfo, filteredSigners)
		if err != nil {
			return currTxDbWrappers, err
		}
		processedTx.Tx.SignerAddresses = signers

		fees, err := a.processor.ProcessFees(indexerTx.AuthInfo, signers)
		if err != nil {
			return currTxDbWrappers, err
		}

		processedTx.Tx.Fees = fees

		// extra fields
		//processedTx.Tx.Signatures = []byte("")
		processedTx.Tx.Memo = ""
		processedTx.Tx.TimeoutHeight = 0

		extensionOptions := make([]string, 0)
		processedTx.Tx.ExtensionOptions = extensionOptions

		nonExtensionOptions := make([]string, 0)
		processedTx.Tx.NonCriticalExtensionOptions = nonExtensionOptions
		processedTx.Tx.TxResponse = model.TxResponse{
			TxHash:    indexerTxResp.TxHash,
			Height:    indexerTxResp.Height,
			TimeStamp: indexerTxResp.TimeStamp,
			Code:      indexerTxResp.Code,
			RawLog:    indexerTxResp.RawLog,
			GasUsed:   indexerTxResp.GasUsed,
			GasWanted: indexerTxResp.GasWanted,
			Codespace: indexerTxResp.Codespace,
		}

		txAuthInfo := model.AuthInfo{
			Fee: model.AuthInfoFee{
				Granter:  authInfo.Fee.Granter,
				Payer:    authInfo.Fee.Payer,
				GasLimit: authInfo.Fee.GasLimit,
			},
			SignerInfos: signerInfos,
		}
		if authInfo.Tip != nil {
			tipAmount := make([]model.TipAmount, 0)
			for _, a := range authInfo.Tip.Amount {
				tipAmount = append(tipAmount, model.TipAmount{
					Denom:  a.Denom,
					Amount: decimal.NewFromInt(a.Amount.Int64()),
				})
			}
			txAuthInfo.Tip = model.Tip{
				Tipper: authInfo.Tip.Tipper,
				Amount: tipAmount,
			}
		}

		processedTx.Tx.AuthInfo = txAuthInfo

		currTxDbWrappers[txIdx] = processedTx
	}

	return currTxDbWrappers, nil
}
