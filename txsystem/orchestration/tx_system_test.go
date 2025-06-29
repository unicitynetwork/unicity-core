package orchestration

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/unicitynetwork/bft-go-base/predicates/templates"
	orchid "github.com/unicitynetwork/bft-go-base/testutils/orchestration"
	"github.com/unicitynetwork/bft-go-base/txsystem/orchestration"
	"github.com/unicitynetwork/bft-go-base/types"

	"github.com/unicitynetwork/bft-core/internal/testutils/observability"
	testsig "github.com/unicitynetwork/bft-core/internal/testutils/sig"
	"github.com/unicitynetwork/bft-core/state"
	"github.com/unicitynetwork/bft-core/txsystem"
	testtransaction "github.com/unicitynetwork/bft-core/txsystem/testutils/transaction"
)

func TestNewTxSystem_OK(t *testing.T) {
	signer, verifier := testsig.CreateSignerAndVerifier(t)
	s := state.NewEmptyState()
	pubKey, err := verifier.MarshalPublicKey()
	require.NoError(t, err)
	pdr := types.PartitionDescriptionRecord{
		Version:         1,
		NetworkID:       5,
		PartitionID:     orchestration.DefaultPartitionID,
		PartitionTypeID: orchestration.PartitionTypeID,
		TypeIDLen:       8,
		UnitIDLen:       256,
		T2Timeout:       2000 * time.Millisecond,
	}
	txSystem, err := NewTxSystem(
		pdr,
		observability.Default(t),
		WithHashAlgorithm(crypto.SHA256),
		WithState(s),
		WithOwnerPredicate(templates.NewP2pkh256BytesFromKey(pubKey)),
	)
	require.NoError(t, err)
	require.NotNil(t, txSystem)

	unitID, err := pdr.ComposeUnitID(types.ShardID{}, orchestration.VarUnitType, orchid.Random)
	require.NoError(t, err)
	roundNumber := uint64(10)
	txo, _ := createAddVarTx(t, signer, &orchestration.AddVarAttributes{},
		testtransaction.WithUnitID(unitID),
		testtransaction.WithClientMetadata(&types.ClientMetadata{Timeout: roundNumber + 1}),
	)

	err = txSystem.BeginBlock(roundNumber)
	require.NoError(t, err)
	txr, err := txSystem.Execute(txo)
	require.NoError(t, err)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{txo.UnitID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee == 0)

	stateSummary, err := txSystem.EndBlock()
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.NoError(t, txSystem.Commit(createUC(stateSummary, roundNumber)))
}

func createUC(s *txsystem.StateSummary, roundNumber uint64) *types.UnicityCertificate {
	return &types.UnicityCertificate{
		Version: 1,
		InputRecord: &types.InputRecord{
			Version:      1,
			RoundNumber:  roundNumber,
			Hash:         s.Root(),
			SummaryValue: s.Summary(),
		},
	}
}
