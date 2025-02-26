package core

import (
	"crypto/sha256"

	"cosmossdk.io/core/transaction"
	sdk "github.com/cosmos/cosmos-sdk/types"
	cosmosTx "github.com/cosmos/cosmos-sdk/types/tx"
	tx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/noders-team/cosmos-indexer/probe"
	"github.com/rs/zerolog/log"
	"google.golang.org/protobuf/reflect/protoreflect"
)

type wrappedTx struct {
	*tx.Tx
	encodedTx []byte
}

func (w *wrappedTx) Hash() [32]byte {
	return sha256.Sum256(w.encodedTx)
}

func (w *wrappedTx) Bytes() []byte {
	return w.encodedTx
}

func (w *wrappedTx) GetMessages() ([]sdk.Msg, error) {
	if w.Body != nil && len(w.Body.Messages) > 0 {
		msgs := make([]sdk.Msg, len(w.Body.Messages))
		for i, anyMsg := range w.Body.Messages {
			msgs[i] = anyMsg
		}
		return msgs, nil
	}
	return nil, nil
}

func (w *wrappedTx) GetReflectMessages() ([]protoreflect.Message, error) {
	/*
		if w.Body != nil && len(w.Body.Messages) > 0 {
			msgs := make([]protoreflect.Message, len(w.Body.Messages))
			for i, anyMsg := range w.Body.Messages {
				msgs[i] = anyMsg
			}
			return msgs, nil
		}*/
	return nil, nil
}

func (w *wrappedTx) GetSenders() ([]transaction.Identity, error) {
	if w.AuthInfo != nil && len(w.AuthInfo.SignerInfos) > 0 {
		senders := make([]transaction.Identity, len(w.AuthInfo.SignerInfos))
		/*
			for i := range w.AuthInfo.SignerInfos {
				// Since we don't have access to the actual addresses here,
				// we return empty strings as placeholders
				senders[i] = w.AuthInfo.SignerInfos[i].GetPublicKey()

			}*/
		return senders, nil
	}
	return nil, nil
}

func (w *wrappedTx) GetGasLimit() (uint64, error) {
	if w.AuthInfo != nil && w.AuthInfo.Fee != nil {
		return w.AuthInfo.Fee.GasLimit, nil
	}
	return 0, nil
}

func (w *wrappedTx) ValidateBasic() error {
	return nil // TODO: implement if needed
}

// InAppTxDecoder Provides an in-app tx decoder.
// The primary use-case for this function is to allow fallback decoding if a TX fails to decode after RPC requests.
// This can happen in a number of scenarios, but mainly due to missing proto definitions.
/*
func InAppTxDecoder(cdc probe.Codec) sdk.TxDecoder {
	return func(txBytes []byte) (*cosmosTx.Tx, error) {
		var raw tx.TxRaw
		var err error

		err = cdc.Marshaler.Unmarshal(txBytes, &raw)
		if err != nil {
			return nil, err
		}

		var body tx.TxBody
		if err = cdc.Marshaler.Unmarshal(raw.BodyBytes, &body); err != nil {
			log.Err(err).Msgf("failed to unmarshal body, transaction will be ignored")
		}

		var authInfo tx.AuthInfo
		err = cdc.Marshaler.Unmarshal(raw.AuthInfoBytes, &authInfo)
		if err != nil {
			log.Err(err).Msgf("failed to unmarshal auth info, transaction will be ignored")
		}

		theTx := tx.Tx{
			Body:       &body,
			AuthInfo:   &authInfo,
			Signatures: raw.Signatures,
		}

		return wrappedTx{}, nil
	}
}*/

func Decode(cdc probe.Codec, txBytes []byte) (*cosmosTx.Tx, error) {
	var raw tx.TxRaw
	var err error

	err = cdc.Marshaler.Unmarshal(txBytes, &raw)
	if err != nil {
		return nil, err
	}

	var body tx.TxBody
	if err = cdc.Marshaler.Unmarshal(raw.BodyBytes, &body); err != nil {
		log.Err(err).Msgf("failed to unmarshal body, transaction will be ignored")
	}

	var authInfo tx.AuthInfo
	err = cdc.Marshaler.Unmarshal(raw.AuthInfoBytes, &authInfo)
	if err != nil {
		log.Err(err).Msgf("failed to unmarshal auth info, transaction will be ignored")
	}

	theTx := tx.Tx{
		Body:       &body,
		AuthInfo:   &authInfo,
		Signatures: raw.Signatures,
	}

	return &theTx, nil
}
