package permissioned

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/unicitynetwork/bft-core/internal/testutils/observability"
	testsig "github.com/unicitynetwork/bft-core/internal/testutils/sig"
	"github.com/unicitynetwork/bft-core/state"
	"github.com/unicitynetwork/bft-core/tree/avl"
	testctx "github.com/unicitynetwork/bft-core/txsystem/testutils/exec_context"
	"github.com/unicitynetwork/bft-go-base/crypto"
	"github.com/unicitynetwork/bft-go-base/predicates/templates"
	moneyid "github.com/unicitynetwork/bft-go-base/testutils/money"
	"github.com/unicitynetwork/bft-go-base/txsystem/fc"
	"github.com/unicitynetwork/bft-go-base/txsystem/fc/permissioned"
	"github.com/unicitynetwork/bft-go-base/types"
)

func TestValidateDeleteFCR(t *testing.T) {
	// generate keys
	adminKeySigner, adminKeyVerifier := testsig.CreateSignerAndVerifier(t)
	adminPubKey, err := adminKeyVerifier.MarshalPublicKey()
	require.NoError(t, err)

	_, userKeyVerifier := testsig.CreateSignerAndVerifier(t)
	userPubKey, err := userKeyVerifier.MarshalPublicKey()
	require.NoError(t, err)

	// create fee credit module
	stateTree := state.NewEmptyState()
	targetPDR := moneyid.PDR()
	partitionID := types.PartitionID(5)
	const fcrUnitType = 1
	adminOwnerPredicate := templates.NewP2pkh256BytesFromKey(adminPubKey)
	m, err := NewFeeCreditModule(targetPDR, stateTree, fcrUnitType, adminOwnerPredicate, observability.Default(t))
	require.NoError(t, err)

	// common default values used in each test
	fcrOwnerPredicate := templates.NewP2pkh256BytesFromKey(userPubKey)
	timeout := uint64(10)
	fcrID, err := targetPDR.ComposeUnitID(types.ShardID{}, fcrUnitType, fc.PrndSh(fcrOwnerPredicate, timeout))
	require.NoError(t, err)

	t.Run("ok", func(t *testing.T) {
		tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, nil, nil)
		require.NoError(t, err)
		fcrUnit := state.NewUnit(&fc.FeeCreditRecord{Balance: 1e8, MinLifetime: timeout, OwnerPredicate: fcrOwnerPredicate})
		exeCtx := testctx.NewMockExecutionContext(testctx.WithUnit(fcrUnit))
		err = m.validateDeleteFC(tx, attr, authProof, exeCtx)
		require.NoError(t, err)
	})

	t.Run("FeeCreditRecordID is not nil", func(t *testing.T) {
		tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, []byte{1}, nil)
		require.NoError(t, err)
		err = m.validateDeleteFC(tx, attr, authProof, testctx.NewMockExecutionContext())
		require.ErrorContains(t, err, "fee transaction cannot contain fee credit reference")
	})

	t.Run("FeeProof is not nil", func(t *testing.T) {
		tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, nil, []byte{1})
		require.NoError(t, err)
		err = m.validateDeleteFC(tx, attr, authProof, testctx.NewMockExecutionContext())
		require.ErrorContains(t, err, "fee transaction cannot contain fee authorization proof")
	})

	t.Run("Invalid unit type byte", func(t *testing.T) {
		// create new fcrID with invalid type byte
		fcrUnitType := []byte{2}
		fcrID, err := targetPDR.ComposeUnitID(types.ShardID{}, uint32(fcrUnitType[0]), fc.PrndSh(fcrOwnerPredicate, timeout))
		require.NoError(t, err)
		tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, nil, nil)
		require.NoError(t, err)
		err = m.validateDeleteFC(tx, attr, authProof, testctx.NewMockExecutionContext())
		require.ErrorContains(t, err, "invalid unit type for unitID")
	})

	t.Run("Fee credit record does not exists", func(t *testing.T) {
		tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, nil, nil)
		require.NoError(t, err)
		err = m.validateDeleteFC(tx, attr, authProof, testctx.NewMockExecutionContext(testctx.WithErr(avl.ErrNotFound)))
		require.ErrorContains(t, err, "failed to get unit: not found")
	})

	t.Run("Invalid signature", func(t *testing.T) {
		// sign tx with random non-admin key
		signer, _ := testsig.CreateSignerAndVerifier(t)
		tx, attr, authProof, err := newDeleteFeeTx(signer, partitionID, fcrID, timeout, nil, nil)
		require.NoError(t, err)
		fcrUnit := state.NewUnit(&fc.FeeCreditRecord{Balance: 1e8, MinLifetime: timeout, OwnerPredicate: fcrOwnerPredicate})
		exeCtx := testctx.NewMockExecutionContext(testctx.WithUnit(fcrUnit))
		err = m.validateDeleteFC(tx, attr, authProof, exeCtx)
		require.ErrorContains(t, err, "invalid owner proof")
	})

	t.Run("Invalid counter", func(t *testing.T) {
		tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, nil, nil)
		require.NoError(t, err)
		fcrUnit := state.NewUnit(&fc.FeeCreditRecord{Balance: 1e8, MinLifetime: timeout, Counter: 1, OwnerPredicate: fcrOwnerPredicate})
		exeCtx := testctx.NewMockExecutionContext(testctx.WithUnit(fcrUnit))
		err = m.validateDeleteFC(tx, attr, authProof, exeCtx)
		require.ErrorContains(t, err, "invalid counter: tx.Counter=0 fcr.Counter=1")
	})
}

func TestExecuteDeleteFCR(t *testing.T) {
	// generate keys
	adminKeySigner, adminKeyVerifier := testsig.CreateSignerAndVerifier(t)
	adminPubKey, err := adminKeyVerifier.MarshalPublicKey()
	require.NoError(t, err)

	_, userKeyVerifier := testsig.CreateSignerAndVerifier(t)
	userPubKey, err := userKeyVerifier.MarshalPublicKey()
	require.NoError(t, err)

	// create fee credit module
	stateTree := state.NewEmptyState()
	targetPDR := moneyid.PDR()
	partitionID := types.PartitionID(5)
	const fcrUnitType = 1
	adminOwnerPredicate := templates.NewP2pkh256BytesFromKey(adminPubKey)
	m, err := NewFeeCreditModule(targetPDR, stateTree, fcrUnitType, adminOwnerPredicate, observability.Default(t))
	require.NoError(t, err)

	// add unit to state tree
	fcrOwnerPredicate := templates.NewP2pkh256BytesFromKey(userPubKey)
	timeout := uint64(10)
	fcrID, err := targetPDR.ComposeUnitID(types.ShardID{}, fcrUnitType, fc.PrndSh(fcrOwnerPredicate, timeout))
	require.NoError(t, err)
	err = stateTree.Apply(state.AddUnit(fcrID, &fc.FeeCreditRecord{}))
	require.NoError(t, err)

	// create tx
	tx, attr, authProof, err := newDeleteFeeTx(adminKeySigner, partitionID, fcrID, timeout, nil, nil)
	require.NoError(t, err)

	// execute tx
	sm, err := m.executeDeleteFC(tx, attr, authProof, testctx.NewMockExecutionContext())
	require.NoError(t, err)
	require.NotNil(t, sm)

	// verify server metadata
	require.EqualValues(t, 0, sm.ActualFee)
	require.Len(t, sm.TargetUnits, 1)
	require.Equal(t, fcrID, sm.TargetUnits[0])
	require.Equal(t, types.TxStatusSuccessful, sm.SuccessIndicator)

	// verify state was updated (unit still exists but owner predicate is set to AlwaysFalse)
	unit, err := stateTree.GetUnit(fcrID, false)
	require.NoError(t, err)
	require.NotNil(t, unit)
	unitData, ok := unit.Data().(*fc.FeeCreditRecord)
	require.True(t, ok)
	require.EqualValues(t, templates.AlwaysFalseBytes(), unitData.OwnerPredicate)
}

func newDeleteFeeTx(adminSigner crypto.Signer, partitionID types.PartitionID, unitID []byte, timeout uint64, fcrID, feeProof []byte) (*types.TransactionOrder, *permissioned.DeleteFeeCreditAttributes, *permissioned.DeleteFeeCreditAuthProof, error) {
	attr := &permissioned.DeleteFeeCreditAttributes{}
	payload, err := newTxPayload(partitionID, permissioned.TransactionTypeDeleteFeeCredit, unitID, fcrID, timeout, nil, attr)
	if err != nil {
		return nil, nil, nil, err
	}
	txo := &types.TransactionOrder{Version: 1, Payload: payload, FeeProof: feeProof}
	authProof, err := signAuthProof(txo, adminSigner, func(ownerProof []byte) *permissioned.DeleteFeeCreditAuthProof {
		return &permissioned.DeleteFeeCreditAuthProof{OwnerProof: ownerProof}
	})
	if err != nil {
		return nil, nil, nil, err
	}
	return txo, attr, authProof, nil
}

func signAuthProof[T any](txo *types.TransactionOrder, signer crypto.Signer, createAuthProof func(ownerProof []byte) T) (T, error) {
	var zeroVal T // To return in case of error

	sigBytes, err := txo.AuthProofSigBytes()
	if err != nil {
		return zeroVal, err
	}

	sig, err := signer.SignBytes(sigBytes)
	if err != nil {
		return zeroVal, err
	}

	adminKeyVerifier, err := signer.Verifier()
	if err != nil {
		return zeroVal, err
	}

	adminPublicKey, err := adminKeyVerifier.MarshalPublicKey()
	if err != nil {
		return zeroVal, err
	}

	ownerProof := templates.NewP2pkh256SignatureBytes(sig, adminPublicKey)
	authProof := createAuthProof(ownerProof)

	if err = txo.SetAuthProof(authProof); err != nil {
		return zeroVal, err
	}
	return authProof, nil
}
