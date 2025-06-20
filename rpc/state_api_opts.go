package rpc

import (
	"github.com/unicitynetwork/bft-core/partition"
	"github.com/unicitynetwork/bft-go-base/types"
)

type (
	StateAPIOptions struct {
		withGetUnits      bool
		shardConf         *types.PartitionDescriptionRecord
		ownerIndex        partition.IndexReader
		rateLimit         int
		responseItemLimit int
	}

	StateAPIOption func(*StateAPIOptions)
)

func WithGetUnits(withGetUnits bool) StateAPIOption {
	return func(c *StateAPIOptions) {
		c.withGetUnits = withGetUnits
	}
}

func WithShardConf(shardConf *types.PartitionDescriptionRecord) StateAPIOption {
	return func(c *StateAPIOptions) {
		c.shardConf = shardConf
	}
}

func WithOwnerIndex(ownerIndex partition.IndexReader) StateAPIOption {
	return func(c *StateAPIOptions) {
		c.ownerIndex = ownerIndex
	}
}

func WithRateLimit(rateLimit int) StateAPIOption {
	return func(c *StateAPIOptions) {
		c.rateLimit = rateLimit
	}
}

func WithResponseItemLimit(limit int) StateAPIOption {
	return func(c *StateAPIOptions) {
		c.responseItemLimit = limit
	}
}

func defaultStateAPIOptions() *StateAPIOptions {
	return &StateAPIOptions{
		withGetUnits:      false,
		shardConf:         nil,
		ownerIndex:        nil,
		rateLimit:         0,
		responseItemLimit: 0,
	}
}
