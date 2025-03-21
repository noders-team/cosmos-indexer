package db

import (
	"encoding/base64"
	"encoding/json"
	"time"

	"github.com/noders-team/cosmos-indexer/db/models"
	"github.com/noders-team/cosmos-indexer/parsers"
	"github.com/noders-team/cosmos-indexer/pkg/model"
)

type BlockDBWrapper struct {
	Block                         *models.Block
	BeginBlockEvents              []BlockEventDBWrapper
	EndBlockEvents                []BlockEventDBWrapper
	UniqueBlockEventTypes         map[string]models.BlockEventType
	UniqueBlockEventAttributeKeys map[string]models.BlockEventAttributeKey
}

type BlockEventDBWrapper struct {
	BlockEvent               models.BlockEvent
	Attributes               []models.BlockEventAttribute
	BlockEventParsedDatasets []parsers.BlockEventParsedData
}

type TxDBWrapper struct {
	Tx                         model.Tx
	Messages                   []MessageDBWrapper
	UniqueMessageTypes         map[string]model.MessageType
	UniqueMessageEventTypes    map[string]model.MessageEventType
	UniqueMessageAttributeKeys map[string]model.MessageEventAttributeKey
}

type MessageDBWrapper struct {
	Message       model.Message
	MessageEvents []MessageEventDBWrapper
}

type MessageEventDBWrapper struct {
	MessageEvent model.MessageEvent
	Attributes   []model.MessageEventAttribute
}

type DenomDBWrapper struct {
	Denom models.Denom
}

type EvmTransaction struct {
	Hash          string
	From          string
	To            string
	Value         string
	Data          []byte
	Gas           uint64
	GasPrice      string
	Nonce         uint64
	BlockHash     string
	BlockNumber   int64
	TxIndex       uint
	Status        uint64
	Timestamp     time.Time
	TokenTransfer *EvmTokenTransfer
}

type EvmTokenTransfer struct {
	Amount   string
	Address  string
	Receiver string
}

func (tx *EvmTransaction) MarshalJSON() ([]byte, error) {
	data := map[string]interface{}{
		"hash":         tx.Hash,
		"from":         tx.From,
		"to":           tx.To,
		"value":        tx.Value,
		"data":         base64.StdEncoding.EncodeToString(tx.Data),
		"gas":          tx.Gas,
		"gas_price":    tx.GasPrice,
		"nonce":        tx.Nonce,
		"block_hash":   tx.BlockHash,
		"block_number": tx.BlockNumber,
		"tx_index":     tx.TxIndex,
		"status":       tx.Status,
		"timestamp":    tx.Timestamp,
	}
	if tx.TokenTransfer != nil {
		data["token_amount"] = tx.TokenTransfer.Amount
		data["token_address"] = tx.TokenTransfer.Address
		data["token_recipient"] = tx.TokenTransfer.Receiver
	}
	return json.Marshal(data)
}

func (tx *EvmTransaction) UnmarshalJSON(data []byte) error {
	var mapped map[string]interface{}
	err := json.Unmarshal(data, &mapped)
	if err != nil {
		return err
	}

	if hash, ok := mapped["hash"].(string); ok {
		tx.Hash = hash
	}
	if from, ok := mapped["from"].(string); ok {
		tx.From = from
	}
	if to, ok := mapped["to"].(string); ok {
		tx.To = to
	}
	if value, ok := mapped["value"].(string); ok {
		tx.Value = value
	}
	if dataStr, ok := mapped["data"].(string); ok {
		tx.Data, _ = base64.StdEncoding.DecodeString(dataStr)
	}
	if gas, ok := mapped["gas"].(float64); ok {
		tx.Gas = uint64(gas)
	}
	if gasPrice, ok := mapped["gas_price"].(string); ok {
		tx.GasPrice = gasPrice
	}
	if nonce, ok := mapped["nonce"].(float64); ok {
		tx.Nonce = uint64(nonce)
	}
	if blockHash, ok := mapped["block_hash"].(string); ok {
		tx.BlockHash = blockHash
	}
	if blockNumber, ok := mapped["block_number"].(float64); ok {
		tx.BlockNumber = int64(blockNumber)
	}
	if txIndex, ok := mapped["tx_index"].(float64); ok {
		tx.TxIndex = uint(txIndex)
	}
	if status, ok := mapped["status"].(float64); ok {
		tx.Status = uint64(status)
	}
	if timestamp, ok := mapped["timestamp"].(string); ok {
		tx.Timestamp, _ = time.Parse(time.RFC3339, timestamp)
	}

	evmTransfer := &EvmTokenTransfer{}
	if tokenAmount, ok := mapped["token_amount"].(string); ok {
		evmTransfer.Amount = tokenAmount
	}
	if tokenAddress, ok := mapped["token_address"].(string); ok {
		evmTransfer.Address = tokenAddress
	}
	if tokenRecipient, ok := mapped["token_recipient"].(string); ok {
		evmTransfer.Receiver = tokenRecipient
	}
	tx.TokenTransfer = evmTransfer

	return nil
}
