package probe

import (
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/codec/types"
	//"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	//"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	//cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/std"
	// sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/tx"
)

type Codec struct {
	InterfaceRegistry types.InterfaceRegistry
	Marshaler         codec.Codec
	TxConfig          client.TxConfig
	Amino             *codec.LegacyAmino
}

func MakeCodec(modules []module.AppModule) Codec {
	//modBasic := module.NewBasicManager()
	//for _, mb := range moduleBasics {
	//	modBasic.RegisterModule(mb)
	//}
	encCfg := MakeCodecConfig()

	// std.RegisterLegacyAminoCodec(encCfg.Amino)
	// std.RegisterInterfaces(encCfg.InterfaceRegistry)
	// modBasic.RegisterLegacyAminoCodec(encodingConfig.Amino)
	// modBasic.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	mb := module.NewManager(modules...)
	// std.RegisterLegacyAminoCodec(encCfg.Amino)
	std.RegisterInterfaces(encCfg.InterfaceRegistry)
	mb.RegisterLegacyAminoCodec(encCfg.Amino)
	mb.RegisterInterfaces(encCfg.InterfaceRegistry)

	return encCfg
}

// Split out from base codec to not include explicitly.
// Should be included only when needed.
func RegisterOsmosisInterfaces(registry types.InterfaceRegistry) {
	// Needs to be extended in order to cover all the modules
}

// Split out from base codec to not include explicitly.
// Should be included only when needed.
func RegisterTendermintLiquidityInterfaces(aminoCodec *codec.LegacyAmino, registry types.InterfaceRegistry) {
}

func MakeCodecConfig() Codec {
	ir := types.NewInterfaceRegistry()
	// interfaceRegistry.RegisterInterface("cosmos.crypto.Pubkey", (*cryptotypes.PubKey)(nil))
	// interfaceRegistry.RegisterImplementations((*cryptotypes.PubKey)(nil), &ed25519.PubKey{})
	// interfaceRegistry.RegisterImplementations((*cryptotypes.PubKey)(nil), &secp256k1.PubKey{})
	// interfaceRegistry.RegisterImplementations((*cryptotypes.PubKey)(nil), &multisig.LegacyAminoPubKey{})

	cdc := codec.NewProtoCodec(ir)

	signingCtx := cdc.InterfaceRegistry().SigningContext()

	signModes := []signing.SignMode{
		signing.SignMode_SIGN_MODE_DIRECT,
		// signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON,
	}

	return Codec{
		InterfaceRegistry: ir,
		Marshaler:         cdc,
		TxConfig: tx.NewTxConfig(cdc,
			signingCtx.AddressCodec(),
			signingCtx.ValidatorAddressCodec(),
			signModes),
		Amino: codec.NewLegacyAmino(),
	}
}
