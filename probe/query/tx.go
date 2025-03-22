package query

import (
	"context"
	"fmt"

	"github.com/noders-team/cosmos-indexer/probe"

	txTypes "github.com/cosmos/cosmos-sdk/types/tx"
)

// TxsAtHeightRPC Get Transactions for the given block height.
// Other query options can be specified with the GetTxsEventRequest.
//
// RPC endpoint is defined in cosmos-sdk: proto/cosmos/tx/v1beta1/service.proto,
// See GetTxsEvent(GetTxsEventRequest) returns (GetTxsEventResponse)
func TxsAtHeightRPC(q *Query, height int64, codec probe.Codec, page, limit uint64) (*txTypes.GetTxsEventResponse, error) {
	orderBy := txTypes.OrderBy_ORDER_BY_UNSPECIFIED

	req := &txTypes.GetTxsEventRequest{
		// Events:  []string{"tx.height=" + fmt.Sprintf("%d", height)},
		Query:   fmt.Sprintf("tx.height=%d", height),
		Events:  []string{fmt.Sprintf("tx.height=%d", height)},
		Page:    page,
		Limit:   limit,
		OrderBy: orderBy,
	}
	return TxsRPC(q, req, codec)
}

// TxsRPC Get Transactions for the given block height.
// Other query options can be specified with the GetTxsEventRequest.
//
// RPC endpoint is defined in cosmos-sdk: proto/cosmos/tx/v1beta1/service.proto,
// See GetTxsEvent(GetTxsEventRequest) returns (GetTxsEventResponse)
func TxsRPC(q *Query, req *txTypes.GetTxsEventRequest, codec probe.Codec) (*txTypes.GetTxsEventResponse, error) {
	queryClient := txTypes.NewServiceClient(q.Client)
	ctx, cancel := q.GetQueryContext()
	defer cancel()

	res, err := queryClient.GetTxsEvent(ctx, req)
	if err != nil {
		return nil, err
	}

	for _, tx := range res.GetTxs() {
		// BUG: This function errors out on the first type error, meaning that the first message that fails ends the unpacking process
		// Since TXs can have multiple messages, this means that we won't be able to process the messages that come after the first error
		// We may want to pull out the unpacking logic into a separate function that can be called on each message individually but not fail hard
		tx.UnpackInterfaces(codec.InterfaceRegistry)
	}

	return res, nil
}

func TxDecode(ctx context.Context, q *Query, tx *[]byte) (*txTypes.TxDecodeResponse, error) {
	queryClient := txTypes.NewServiceClient(q.Client)
	ctx, cancel := q.GetQueryContext()
	defer cancel()

	res, err := queryClient.TxDecode(ctx, &txTypes.TxDecodeRequest{TxBytes: *tx})
	if err != nil {
		return nil, err
	}

	res2, err := queryClient.GetTx(ctx, &txTypes.GetTxRequest{Hash: "16BDEABEEFA07FB1E9253808533919408FB74EC3FE1EBFBF0BB69DDA4D7D33A7"})
	if err != nil {
		return nil, err
	}
	print(res2)

	return res, nil
}
