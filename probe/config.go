package probe

import (
	"github.com/cosmos/cosmos-sdk/types/module"
)

// Provides a default set of AppModuleBasics that are included in the ChainClientConfig
// This is used to provide a default set of modules that will be used for protobuf registration and in-app decoding of RPC responses
var DefaultModuleBasics = []module.AppModule{
	// auth.AppModuleBasic{},
	// authz.AppModuleBasic{},
	// bank.AppModuleBasic{},
	// capability.AppModuleBasic{},
	// gov.AppModuleBasic{},
	// crisis.AppModuleBasic{},
	// distribution.AppModuleBasic{},
	// feegrant.AppModuleBasic{},
	// mint.AppModuleBasic{},
	// params.AppModuleBasic{},
	// slashing.AppModuleBasic{},
	// staking.AppModuleBasic{},
	// vesting.AppModuleBasic{},
}

type ChainClientConfig struct {
	Key            string             `json:"key" yaml:"key"`
	ChainID        string             `json:"chain-id" yaml:"chain-id"`
	RPCAddr        string             `json:"rpc-addr" yaml:"rpc-addr"`
	AccountPrefix  string             `json:"account-prefix" yaml:"account-prefix"`
	KeyringBackend string             `json:"keyring-backend" yaml:"keyring-backend"`
	KeyDirectory   string             `json:"key-directory" yaml:"key-directory"`
	Debug          bool               `json:"debug" yaml:"debug"`
	Timeout        string             `json:"timeout" yaml:"timeout"`
	OutputFormat   string             `json:"output-format" yaml:"output-format"`
	Modules        []module.AppModule `json:"-" yaml:"-"`
	EvmRestURl     string             `json:"-" yaml:"-"`
}
