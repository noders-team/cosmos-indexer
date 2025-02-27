package db

import (
	"encoding/base64"
	"encoding/json"
	"github.com/noders-team/cosmos-indexer/db/models"
	"github.com/noders-team/cosmos-indexer/parsers"
	"github.com/noders-team/cosmos-indexer/pkg/model"
	"time"
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

func (tx *EvmTransaction) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]interface{}{
		"Hash":        tx.Hash,
		"From":        tx.From,
		"To":          tx.To,
		"Value":       tx.Value,
		"Data":        base64.StdEncoding.EncodeToString(tx.Data),
		"Gas":         tx.Gas,
		"GasPrice":    tx.GasPrice,
		"Nonce":       tx.Nonce,
		"BlockHash":   tx.BlockHash,
		"BlockNumber": tx.BlockNumber,
		"TxIndex":     tx.TxIndex,
		"Status":      tx.Status,
		"Timestamp":   tx.Timestamp,
	})
}

func (tx *EvmTransaction) UnmarshalJSON(data []byte) error {
	var mapped map[string]interface{}
	err := json.Unmarshal(data, &mapped)
	if err != nil {
		return err
	}

	if hash, ok := mapped["Hash"].(string); ok {
		tx.Hash = hash
	}
	if from, ok := mapped["From"].(string); ok {
		tx.From = from
	}
	if to, ok := mapped["To"].(string); ok {
		tx.To = to
	}
	if value, ok := mapped["Value"].(string); ok {
		tx.Value = value
	}
	if dataStr, ok := mapped["Data"].(string); ok {
		tx.Data, _ = base64.StdEncoding.DecodeString(dataStr)
	}
	if gas, ok := mapped["Gas"].(float64); ok {
		tx.Gas = uint64(gas)
	}
	if gasPrice, ok := mapped["GasPrice"].(string); ok {
		tx.GasPrice = gasPrice
	}
	if nonce, ok := mapped["Nonce"].(float64); ok {
		tx.Nonce = uint64(nonce)
	}
	if blockHash, ok := mapped["BlockHash"].(string); ok {
		tx.BlockHash = blockHash
	}
	if blockNumber, ok := mapped["BlockNumber"].(float64); ok {
		tx.BlockNumber = int64(blockNumber)
	}
	if txIndex, ok := mapped["TxIndex"].(float64); ok {
		tx.TxIndex = uint(txIndex)
	}
	if status, ok := mapped["Status"].(float64); ok {
		tx.Status = uint64(status)
	}
	if timestamp, ok := mapped["Timestamp"].(string); ok {
		tx.Timestamp, _ = time.Parse(time.RFC3339, timestamp)
	}

	return nil
}
