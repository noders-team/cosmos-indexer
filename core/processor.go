package core

import (
	"encoding/hex"
	"fmt"

	"github.com/rs/zerolog/log"

	ctypes "github.com/cometbft/cometbft/rpc/core/types"
	sdkTypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/noders-team/cosmos-indexer/config"
	"github.com/noders-team/cosmos-indexer/db/models"
)

type BlockProcessingFailure int

const (
	NodeMissingBlockTxs BlockProcessingFailure = iota
	BlockQueryError
	UnprocessableTxError
	OsmosisNodeRewardLookupError
	OsmosisNodeRewardIndexError
	NodeMissingHistoryForBlock
	FailedBlockEventHandling
)

type FailedBlockHandler func(height int64, code BlockProcessingFailure, err error)

// ProcessBlock Process RPC Block data into the model object used by the application.
func ProcessBlock(blockData *ctypes.ResultBlock, blockResultsData *ctypes.ResultBlockResults, chainID uint) (models.Block, error) {
	block := models.Block{
		Height:  blockData.Block.Height,
		ChainID: chainID,
	}

	propAddressFromHex, err := sdkTypes.ConsAddressFromHex(
		hex.EncodeToString(blockData.Block.ProposerAddress.Bytes()))
	if err != nil {
		return block, err
	}

	block.ProposerConsAddress = models.Address{Address: propAddressFromHex.String()}
	block.TimeStamp = blockData.Block.Time
	block.BlockHash = blockData.BlockID.Hash.String()
	block.TotalTxs = blockData.Block.Txs.Len()

	signatures := make([]models.BlockSignature, 0)
	for _, bl := range blockData.Block.LastCommit.Signatures {
		if len(bl.ValidatorAddress.Bytes()) == 0 {
			log.Warn().Msgf("validator address is empty, ignoring %d", blockData.Block.Height)
			continue
		}
		address, err := sdkTypes.ConsAddressFromHex(
			hex.EncodeToString(bl.ValidatorAddress.Bytes()))
		if err != nil {
			log.Err(err).Msgf("Error parsing validator address %s", bl.ValidatorAddress.String())
			continue
		}

		sigTime := bl.Timestamp
		if sigTime.IsZero() {
			sigTime = blockData.Block.Time
		}

		signatures = append(signatures, models.BlockSignature{
			ValidatorAddress: address.String(),
			Signature:        bl.Signature,
			Timestamp:        sigTime,
		})
	}
	if len(signatures) == 0 {
		log.Warn().Msgf("No signatures found for block %d", blockData.Block.Height)
	}

	block.Signatures = signatures

	return block, nil
}

// Log error to stdout. Not much else we can do to handle right now.
func HandleFailedBlock(height int64, code BlockProcessingFailure, err error) {
	reason := "{unknown error}"
	switch code {
	case NodeMissingBlockTxs:
		reason = "node has no TX history for block"
	case BlockQueryError:
		reason = "failed to query block result for block"
	case OsmosisNodeRewardLookupError:
		reason = "Failed Osmosis rewards lookup for block"
	case OsmosisNodeRewardIndexError:
		reason = "Failed Osmosis rewards indexing for block"
	case NodeMissingHistoryForBlock:
		reason = "Node has no TX history for block"
	case FailedBlockEventHandling:
		reason = "Failed to process block event"
	}

	config.Log.Error(fmt.Sprintf("Block %v failed. Reason: %v", height, reason), err)
}
