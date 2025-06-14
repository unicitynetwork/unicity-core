package tokens

import (
	"testing"

	"github.com/stretchr/testify/require"

	test "github.com/unicitynetwork/bft-core/internal/testutils"
	testsig "github.com/unicitynetwork/bft-core/internal/testutils/sig"
	"github.com/unicitynetwork/bft-core/state"
	"github.com/unicitynetwork/bft-core/txsystem"
	testtransaction "github.com/unicitynetwork/bft-core/txsystem/testutils/transaction"
	"github.com/unicitynetwork/bft-go-base/predicates/templates"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
)

// TestTransferNFT_StateLock locks NFT with a transfer tx to pk1, then unlocks it with an update tx
func TestTransferNFT_StateLock(t *testing.T) {
	w1Signer, w1PubKey := createSigner(t)
	txs, _, pdr := newTokenTxSystem(t)
	unitID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT to pk1 with state lock
	transferTx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(unitID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: templates.NewP2pkh256BytesFromKey(w1PubKey),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(tokens.TransferNonFungibleTokenAuthProof{
			TokenTypeOwnerProofs: [][]byte{templates.EmptyArgument()}},
		),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
		testtransaction.WithStateLock(&types.StateLock{
			ExecutionPredicate: templates.NewP2pkh256BytesFromKey(w1PubKey),
			RollbackPredicate:  templates.NewP2pkh256BytesFromKey(w1PubKey)},
		),
	)
	_, err := txs.Execute(transferTx)
	require.NoError(t, err)

	// verify unit was locked and bearer hasn't changed
	u, err := txs.State().GetUnit(unitID, false)
	require.NoError(t, err)
	unit, err := state.ToUnitV1(u)
	require.NoError(t, err)
	require.True(t, unit.IsStateLocked())

	require.IsType(t, &tokens.NonFungibleTokenData{}, u.Data())
	d := u.Data().(*tokens.NonFungibleTokenData)
	require.Equal(t, nftTypeID2, d.TypeID)
	require.EqualValues(t, []byte{0xa}, d.Data)
	require.Equal(t, uint64(0), d.Counter)
	require.EqualValues(t, templates.AlwaysTrueBytes(), d.Owner())

	// try to update nft without state unlocking
	updateTx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(unitID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Data:    test.RandomBytes(10),
			Counter: 1,
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(updateTx)
	require.NoError(t, err)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)

	// update nft with state unlock, it must be transferred to new bearer w1
	attr := &tokens.UpdateNonFungibleTokenAttributes{
		Data:    []byte{42},
		Counter: 1,
	}
	updateTx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(unitID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(attr),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(templates.EmptyArgument()),
		testtransaction.WithAuthProof(
			tokens.UpdateNonFungibleTokenAuthProof{TokenTypeDataUpdateProofs: [][]byte{templates.EmptyArgument()}},
		),
	)
	ownerProof := testsig.NewStateLockProofSignature(t, updateTx, w1Signer)
	updateTx.StateUnlock = append([]byte{byte(txsystem.StateUnlockExecute)}, ownerProof...)

	_, err = txs.Execute(updateTx)
	require.NoError(t, err)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)

	// verify unit was unlocked and bearer has changed
	u, err = txs.State().GetUnit(unitID, false)
	require.NoError(t, err)
	unit, err = state.ToUnitV1(u)
	require.NoError(t, err)
	require.False(t, unit.IsStateLocked())

	require.IsType(t, &tokens.NonFungibleTokenData{}, u.Data())
	d = u.Data().(*tokens.NonFungibleTokenData)
	require.Equal(t, nftTypeID2, d.TypeID)
	require.EqualValues(t, attr.Data, d.Data)
	require.Equal(t, uint64(2), d.Counter)
	require.EqualValues(t, templates.NewP2pkh256BytesFromKey(w1PubKey), d.Owner())
}
