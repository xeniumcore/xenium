package app

import "xenium/core"

type Config struct {
	Chain   core.ChainConfig
	DataDir string
}

func DefaultConfig() Config {
	return Config{
		Chain: core.ChainConfig{
			MaxReorgDepth:        2,
			FinalitySlots:        2,
			MinReorgWeightDeltaP: 10,
			EpochLength:          50,
			MaxBlockTxs:          100,
		},
		DataDir: "data",
	}
}
