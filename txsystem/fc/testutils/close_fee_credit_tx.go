package testutils

import (
	"testing"

	"github.com/stretchr/testify/require"
	testtransaction "github.com/unicitynetwork/bft-core/txsystem/testutils/transaction"
	abcrypto "github.com/unicitynetwork/bft-go-base/crypto"
	"github.com/unicitynetwork/bft-go-base/txsystem/fc"
	"github.com/unicitynetwork/bft-go-base/types"
)

func NewCloseFC(t *testing.T, signer abcrypto.Signer, attr *fc.CloseFeeCreditAttributes, opts ...testtransaction.Option) *types.TransactionOrder {
	if attr == nil {
		attr = NewCloseFCAttr()
	}
	tx := testtransaction.NewTransactionOrder(t,
		testtransaction.WithUnitID(NewFeeCreditRecordID(t, signer)),
		testtransaction.WithAttributes(attr),
		testtransaction.WithTransactionType(fc.TransactionTypeCloseFeeCredit),
		testtransaction.WithAuthProof(fc.CloseFeeCreditAuthProof{}),
	)
	for _, opt := range opts {
		require.NoError(t, opt(tx))
	}
	return tx
}

type CloseFCOption func(*fc.CloseFeeCreditAttributes) CloseFCOption

func NewCloseFCAttr(opts ...CloseFCOption) *fc.CloseFeeCreditAttributes {
	defaultTx := NewDefaultCloseFCAttr()
	for _, opt := range opts {
		opt(defaultTx)
	}
	return defaultTx
}

func NewDefaultCloseFCAttr() *fc.CloseFeeCreditAttributes {
	return &fc.CloseFeeCreditAttributes{
		Amount:            amount,
		TargetUnitID:      DefaultMoneyUnitID(),
		TargetUnitCounter: targetUnitCounter,
	}
}

func WithCloseFCAmount(amount uint64) CloseFCOption {
	return func(tx *fc.CloseFeeCreditAttributes) CloseFCOption {
		tx.Amount = amount
		return nil
	}
}

func WithCloseFCCounter(counter uint64) CloseFCOption {
	return func(tx *fc.CloseFeeCreditAttributes) CloseFCOption {
		tx.Counter = counter
		return nil
	}
}

func WithCloseFCTargetUnitID(targetUnitID []byte) CloseFCOption {
	return func(tx *fc.CloseFeeCreditAttributes) CloseFCOption {
		tx.TargetUnitID = targetUnitID
		return nil
	}
}

func WithCloseFCTargetUnitCounter(counter uint64) CloseFCOption {
	return func(tx *fc.CloseFeeCreditAttributes) CloseFCOption {
		tx.TargetUnitCounter = counter
		return nil
	}
}
