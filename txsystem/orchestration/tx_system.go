package orchestration

import (
	"fmt"

	"github.com/unicitynetwork/bft-core/txsystem"
	txtypes "github.com/unicitynetwork/bft-core/txsystem/types"
	"github.com/unicitynetwork/bft-go-base/types"
)

func NewTxSystem(shardConf types.PartitionDescriptionRecord, observe txsystem.Observability, opts ...Option) (*txsystem.GenericTxSystem, error) {
	options, err := defaultOptions(observe)
	if err != nil {
		return nil, fmt.Errorf("failed to load default configuration: %w", err)
	}
	for _, option := range opts {
		option(options)
	}
	module, err := NewModule(shardConf, options)
	if err != nil {
		return nil, fmt.Errorf("failed to load module: %w", err)
	}
	return txsystem.NewGenericTxSystem(
		shardConf,
		[]txtypes.Module{module},
		observe,
		txsystem.WithHashAlgorithm(options.hashAlgorithm),
		txsystem.WithState(options.state),
		txsystem.WithExecutedTransactions(options.executedTransactions),
	)
}

func NOPFeeCreditValidator(_ txtypes.ExecutionContext, _ *types.TransactionOrder) error {
	return nil
}
