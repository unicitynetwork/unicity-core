package money

import (
	"crypto"
	"errors"

	"github.com/unicitynetwork/bft-core/predicates"
	"github.com/unicitynetwork/bft-core/state"
	txtypes "github.com/unicitynetwork/bft-core/txsystem/types"
	fcsdk "github.com/unicitynetwork/bft-go-base/txsystem/fc"
	"github.com/unicitynetwork/bft-go-base/txsystem/money"
	"github.com/unicitynetwork/bft-go-base/txsystem/nop"
	"github.com/unicitynetwork/bft-go-base/types"
)

var _ txtypes.Module = (*Module)(nil)

type (
	Module struct {
		state               *state.State
		trustBase           types.RootTrustBase
		hashAlgorithm       crypto.Hash
		dustCollector       *DustCollector
		feeCreditTxRecorder *feeCreditTxRecorder
		execPredicate       predicates.PredicateRunner
		pdr                 types.PartitionDescriptionRecord
	}
)

func NewMoneyModule(pdr types.PartitionDescriptionRecord, options *Options) (*Module, error) {
	if options == nil {
		return nil, errors.New("money module options are missing")
	}
	if options.state == nil {
		return nil, errors.New("state is nil")
	}

	m := &Module{
		state:               options.state,
		pdr:                 pdr,
		trustBase:           options.trustBase,
		hashAlgorithm:       options.hashAlgorithm,
		feeCreditTxRecorder: newFeeCreditTxRecorder(options.state, pdr.PartitionID, nil),
		dustCollector:       NewDustCollector(options.state),
		execPredicate:       predicates.NewPredicateRunner(options.exec),
	}
	return m, nil
}

func (m *Module) TxHandlers() map[uint16]txtypes.TxExecutor {
	return map[uint16]txtypes.TxExecutor{
		// money partition tx handlers
		money.TransactionTypeTransfer: txtypes.NewTxHandler[money.TransferAttributes, money.TransferAuthProof](m.validateTransferTx, m.executeTransferTx),
		money.TransactionTypeSplit:    txtypes.NewTxHandler[money.SplitAttributes, money.SplitAuthProof](m.validateSplitTx, m.executeSplitTx, txtypes.WithTargetUnitsFn(m.splitTxTargetUnits)),
		money.TransactionTypeTransDC:  txtypes.NewTxHandler[money.TransferDCAttributes, money.TransferDCAuthProof](m.validateTransferDCTx, m.executeTransferDCTx),
		money.TransactionTypeSwapDC:   txtypes.NewTxHandler[money.SwapDCAttributes, money.SwapDCAuthProof](m.validateSwapTx, m.executeSwapTx),

		// fee credit related transaction handlers (credit transfers and reclaims only!)
		fcsdk.TransactionTypeTransferFeeCredit: txtypes.NewTxHandler[fcsdk.TransferFeeCreditAttributes, fcsdk.TransferFeeCreditAuthProof](m.validateTransferFCTx, m.executeTransferFCTx),
		fcsdk.TransactionTypeReclaimFeeCredit:  txtypes.NewTxHandler[fcsdk.ReclaimFeeCreditAttributes, fcsdk.ReclaimFeeCreditAuthProof](m.validateReclaimFCTx, m.executeReclaimFCTx),

		// NOP transaction handler
		nop.TransactionTypeNOP: txtypes.NewTxHandler[nop.Attributes, nop.AuthProof](m.validateNopTx, m.executeNopTx),
	}
}

func (m *Module) BeginBlockFuncs() []func(blockNr uint64) error {
	return []func(blockNr uint64) error{
		func(blockNr uint64) error {
			m.feeCreditTxRecorder.reset()
			return nil
		},
	}
}

func (m *Module) EndBlockFuncs() []func(blockNumber uint64) error {
	return []func(blockNumber uint64) error{
		// m.dustCollector.consolidateDust TODO AB-1133
		// TODO AB-1133 delete bills from owner index (partition/proof_indexer.go)

		// TODO: FCB-s will be gone, no need to consolidate then
		// func(blockNr uint64) error {
		// 	return m.feeCreditTxRecorder.consolidateFees()
		// },
	}
}
