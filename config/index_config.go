package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

type IndexConfig struct {
	Database           Database
	ConfigFileLocation string
	Base               IndexBase
	Log                log
	Probe              Probe
	Flags              flags
	Server             Server
	RedisConf          RedisConf
	MongoConf          MongoConf
}

type IndexBase struct {
	throttlingBase
	RetryBase
	Mode                       string    `mapstructure:"mode"`
	ModeTopics                 *[]string `mapstructure:"mode-storage-topics"`
	ModeCoolDownMins           int       `mapstructure:"mode-cooldown-mins"`
	ModeCoolDownCount          int       `mapstructure:"mode-cooldown-count"`
	ModeBlocksStep             int64     `mapstructure:"mode-blocks-step"`
	ReindexMessageType         string    `mapstructure:"reindex-message-type"`
	ReattemptFailedBlocks      bool      `mapstructure:"reattempt-failed-blocks"`
	StartBlock                 int64     `mapstructure:"start-block"`
	EndBlock                   int64     `mapstructure:"end-block"`
	BlockInputFile             string    `mapstructure:"block-input-file"`
	ReIndex                    bool      `mapstructure:"reindex"`
	RPCWorkers                 int64     `mapstructure:"rpc-workers"`
	BlockTimer                 int64     `mapstructure:"block-timer"`
	WaitForChain               bool      `mapstructure:"wait-for-chain"`
	WaitForChainDelay          int64     `mapstructure:"wait-for-chain-delay"`
	TransactionIndexingEnabled bool      `mapstructure:"index-transactions"`
	ExitWhenCaughtUp           bool      `mapstructure:"exit-when-caught-up"`
	FilterFile                 string    `mapstructure:"filter-file"`
}

// Flags for specific, deeper indexing behavior
type flags struct {
	IndexTxMessageRaw bool `mapstructure:"index-tx-message-raw"`
}

func SetupIndexSpecificFlags(conf *IndexConfig, cmd *cobra.Command) {
	// chain indexing
	cmd.PersistentFlags().Int64Var(&conf.Base.StartBlock, "base.start-block", 0, "block to start indexing at (use -1 to resume from highest block indexed)")
	cmd.PersistentFlags().Int64Var(&conf.Base.EndBlock, "base.end-block", -1, "block to stop indexing at (use -1 to index indefinitely")
	cmd.PersistentFlags().StringVar(&conf.Base.BlockInputFile, "base.block-input-file", "", "A file location containing a JSON list of block heights to index. Will override start and end block flags.")
	cmd.PersistentFlags().BoolVar(&conf.Base.ReIndex, "base.reindex", false, "if true, this will re-attempt to index blocks we have already indexed (defaults to false)")
	cmd.PersistentFlags().BoolVar(&conf.Base.ReattemptFailedBlocks, "base.reattempt-failed-blocks", false, "re-enqueue failed blocks for reattempts at startup.")
	cmd.PersistentFlags().StringVar(&conf.Base.ReindexMessageType, "base.reindex-message-type", "", "a Cosmos message type URL. When set, the block enqueue method will reindex all blocks between start and end block that contain this message type.")
	// block event indexing
	cmd.PersistentFlags().BoolVar(&conf.Base.TransactionIndexingEnabled, "base.index-transactions", false, "enable transaction indexing?")
	// filter configs
	cmd.PersistentFlags().StringVar(&conf.Base.FilterFile, "base.filter-file", "", "path to a file containing a JSON config of block event and message type filters to apply to beginblocker events, endblocker events and TX messages")
	// other base setting
	cmd.PersistentFlags().Int64Var(&conf.Base.ModeBlocksStep, "base.mode-blocks-step", 5000, "block gap for genesis threads")

	cmd.PersistentFlags().Int64Var(&conf.Base.RPCWorkers, "base.rpc-workers", 1, "rpc workers")
	cmd.PersistentFlags().BoolVar(&conf.Base.WaitForChain, "base.wait-for-chain", false, "wait for chain to be in sync?")
	cmd.PersistentFlags().Int64Var(&conf.Base.WaitForChainDelay, "base.wait-for-chain-delay", 10, "seconds to wait between each check for node to catch up to the chain")
	cmd.PersistentFlags().Int64Var(&conf.Base.BlockTimer, "base.block-timer", 10000, "print out how long it takes to process this many blocks")
	cmd.PersistentFlags().BoolVar(&conf.Base.ExitWhenCaughtUp, "base.exit-when-caught-up", false, "mainly used for Osmosis rewards indexing")
	cmd.PersistentFlags().Int64Var(&conf.Base.RequestRetryAttempts, "base.request-retry-attempts", 0, "number of RPC query retries to make")
	cmd.PersistentFlags().Uint64Var(&conf.Base.RequestRetryMaxWait, "base.request-retry-max-wait", 30, "max retry incremental backoff wait time in seconds")

	// flags
	cmd.PersistentFlags().BoolVar(&conf.Flags.IndexTxMessageRaw, "flags.index-tx-message-raw", false, "if true, this will index the raw message bytes. This will significantly increase the size of the database.")
	// run-mode
	cmd.PersistentFlags().StringVar(&conf.Base.Mode, "base.mode", "normal", "running mode, can be normal(default), fetcher or storage")
	conf.Base.ModeTopics = cmd.PersistentFlags().StringArray("base.mode-topics", []string{""}, "topic for fetcher and storage modes")
	cmd.PersistentFlags().IntVar(&conf.Base.ModeCoolDownMins, "base.mode-cooldown-mins", 5, "cool down period for mode fetcher")
	cmd.PersistentFlags().IntVar(&conf.Base.ModeCoolDownCount, "base.mode-cooldown-count", 5000, "cool down count for mode fetcher")
	cmd.PersistentFlags().StringVar(&conf.ConfigFileLocation, "config-file", "", "config file location")
}

func (conf *IndexConfig) Validate() error {
	err := validateDatabaseConf(conf.Database)
	if err != nil {
		return err
	}

	probeConf := conf.Probe

	probeConf, err = validateProbeConf(probeConf)
	if err != nil {
		return err
	}

	conf.Probe = probeConf

	err = validateThrottlingConf(conf.Base.throttlingBase)
	if err != nil {
		return err
	}

	if !conf.Base.TransactionIndexingEnabled {
		return errors.New("must enable at least one of base.index-transactions")
	}

	// Check for required configs when base indexer is enabled
	if conf.Base.TransactionIndexingEnabled {
		if conf.Base.StartBlock == 0 {
			return errors.New("base.start-block must be set when index-chain is enabled")
		}
		if conf.Base.EndBlock == 0 {
			return errors.New("base.end-block must be set when index-chain is enabled")
		}
	}

	if conf.Base.FilterFile != "" {
		// check if file exists
		if _, err := os.Stat(conf.Base.FilterFile); os.IsNotExist(err) {
			return fmt.Errorf("base.filter-file %s does not exist", conf.Base.FilterFile)
		}
	}

	return nil
}

func CheckSuperfluousIndexKeys(keys []string) []string {
	validKeys := make(map[string]struct{})

	addDatabaseConfigKeys(validKeys)
	addLogConfigKeys(validKeys)
	addProbeConfigKeys(validKeys)

	// add base keys
	for _, key := range getValidConfigKeys(IndexBase{}, "base") {
		validKeys[key] = struct{}{}
	}

	for _, key := range getValidConfigKeys(throttlingBase{}, "base") {
		validKeys[key] = struct{}{}
	}

	for _, key := range getValidConfigKeys(RetryBase{}, "base") {
		validKeys[key] = struct{}{}
	}

	for _, key := range getValidConfigKeys(flags{}, "flags") {
		validKeys[key] = struct{}{}
	}

	// Check keys
	ignoredKeys := make([]string, 0)
	for _, key := range keys {
		if _, ok := validKeys[key]; !ok {
			ignoredKeys = append(ignoredKeys, key)
		}
	}

	return ignoredKeys
}
