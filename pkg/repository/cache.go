package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/noders-team/cosmos-indexer/db/models"

	"github.com/noders-team/cosmos-indexer/pkg/model"
	"github.com/redis/go-redis/v9"
)

const (
	blocksChannel            = "pub/blocks"
	txsChannel               = "pub/txs"
	maxTransactionsCacheSize = 50
	maxBlocksCacheSize       = 50
	transactionsKey          = "c/latest_transactions"
	blocksKey                = "c/latest_blocks"
	totalsKey                = "c/totals"
)

type TransactionsCache interface {
	AddTransaction(ctx context.Context, transaction *model.Tx) error
	GetTransactions(ctx context.Context, start, stop int64) ([]*model.Tx, int64, error)
}

type BlocksCache interface {
	AddBlock(ctx context.Context, info *model.BlockInfo) error
	GetBlocks(ctx context.Context, start, stop int64) ([]*model.BlockInfo, int64, error)
}

type TotalsCache interface {
	AddTotals(ctx context.Context, info *model.AggregatedInfo) error
	GetTotals(ctx context.Context) (*model.AggregatedInfo, error)
}

type PubSubCache interface {
	PublishTx(ctx context.Context, tx *model.Tx) error
	PublishBlock(ctx context.Context, info *models.Block) error
}

type Cache struct {
	rdb *redis.Client
}

func NewCache(rdb *redis.Client) *Cache {
	return &Cache{
		rdb: rdb,
	}
}

func (s *Cache) AddTransaction(ctx context.Context, transaction *model.Tx) error {
	res, err := json.Marshal(transaction)
	if err != nil {
		return err
	}

	if err := s.rdb.LPush(ctx, transactionsKey, string(res)).Err(); err != nil {
		return err
	}

	if err := s.rdb.LTrim(ctx, transactionsKey, 0, maxTransactionsCacheSize).Err(); err != nil {
		return err
	}

	return nil
}

func (s *Cache) GetTransactions(ctx context.Context, start, stop int64) ([]*model.Tx, int64, error) {
	if stop > maxTransactionsCacheSize {
		stop = maxTransactionsCacheSize
	}
	res, err := s.rdb.LRange(ctx, transactionsKey, start, (start+stop)-1).Result()
	if err != nil {
		return nil, 0, err
	}

	var transactions []*model.Tx
	for _, r := range res {
		var tx model.Tx
		if err := json.Unmarshal([]byte(r), &tx); err != nil {
			return nil, 0, err
		}
		transactions = append(transactions, &tx)
	}

	total := int64(0)
	resL := s.rdb.LLen(ctx, transactionsKey)
	if resL.Err() != nil {
		return nil, 0, err
	}
	total, err = resL.Result()
	if resL.Err() != nil {
		return nil, 0, err
	}

	return transactions, total, nil
}

func (s *Cache) PublishBlock(ctx context.Context, info *models.Block) error {
	res, err := json.Marshal(&info)
	if err != nil {
		return err
	}

	return s.rdb.Publish(ctx, blocksChannel, res).Err()
}

func (s *Cache) PublishTx(ctx context.Context, tx *model.Tx) error {
	res, err := json.Marshal(&tx)
	if err != nil {
		return err
	}

	return s.rdb.Publish(ctx, txsChannel, res).Err()
}

func (s *Cache) AddBlock(ctx context.Context, info *model.BlockInfo) error {
	res, err := json.Marshal(info)
	if err != nil {
		return err
	}

	if err := s.rdb.LPush(ctx, blocksKey, string(res)).Err(); err != nil {
		return err
	}

	if err := s.rdb.LTrim(ctx, blocksKey, 0, maxBlocksCacheSize).Err(); err != nil {
		return err
	}

	return nil
}

func (s *Cache) GetBlocks(ctx context.Context, start, stop int64) ([]*model.BlockInfo, int64, error) {
	if stop > maxBlocksCacheSize {
		stop = maxBlocksCacheSize
	}

	res, err := s.rdb.LRange(ctx, blocksKey, start, (start+stop)-1).Result()
	if err != nil {
		return nil, 0, err
	}

	var blcs []*model.BlockInfo
	for _, r := range res {
		var tx model.BlockInfo
		if err := json.Unmarshal([]byte(r), &tx); err != nil {
			return nil, 0, err
		}
		blcs = append(blcs, &tx)
	}

	total := int64(0)
	resL := s.rdb.LLen(ctx, blocksKey)
	if resL.Err() != nil {
		return nil, total, resL.Err()
	}
	total = resL.Val()

	return blcs, total, nil
}

func (s *Cache) AddTotals(ctx context.Context, info *model.AggregatedInfo) error {
	if info == nil {
		return nil
	}

	res, err := json.Marshal(&info)
	if err != nil {
		return err
	}
	return s.rdb.Set(ctx, totalsKey, string(res), 10*time.Minute).Err()
}

func (s *Cache) GetTotals(ctx context.Context) (*model.AggregatedInfo, error) {
	res, err := s.rdb.Get(ctx, totalsKey).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to get totals: %w", err)
	}

	var info model.AggregatedInfo
	if err = json.Unmarshal([]byte(res), &info); err != nil {
		return nil, err
	}
	return &info, err
}
