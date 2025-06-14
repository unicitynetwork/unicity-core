package tokens

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	abcrypto "github.com/unicitynetwork/bft-go-base/crypto"
	abhash "github.com/unicitynetwork/bft-go-base/hash"
	"github.com/unicitynetwork/bft-go-base/predicates/templates"
	tokenid "github.com/unicitynetwork/bft-go-base/testutils/tokens"
	"github.com/unicitynetwork/bft-go-base/txsystem/fc"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
	"github.com/unicitynetwork/bft-go-base/util"

	test "github.com/unicitynetwork/bft-core/internal/testutils"
	"github.com/unicitynetwork/bft-core/internal/testutils/observability"
	testsig "github.com/unicitynetwork/bft-core/internal/testutils/sig"
	testtb "github.com/unicitynetwork/bft-core/internal/testutils/trustbase"
	"github.com/unicitynetwork/bft-core/state"
	"github.com/unicitynetwork/bft-core/txsystem"
	"github.com/unicitynetwork/bft-core/txsystem/fc/testutils"
	testtransaction "github.com/unicitynetwork/bft-core/txsystem/testutils/transaction"
)

const validNFTURI = "https://example.com/nft"

var (
	parent1Identifier types.UnitID = append(make(types.UnitID, 31), 1, tokens.NonFungibleTokenTypeUnitType)
	nftTypeID1        types.UnitID = append(make(types.UnitID, 31), 10, tokens.NonFungibleTokenTypeUnitType)
	nftTypeID2        types.UnitID = append(make(types.UnitID, 31), 11, tokens.NonFungibleTokenTypeUnitType)
	nftName                        = fmt.Sprintf("Long name for %s", nftTypeID1)
)

var (
	symbol                   = "TEST"
	subTypeCreationPredicate = []byte{4}
	tokenMintingPredicate    = []byte{5}
	tokenTypeOwnerPredicate  = []byte{6}
	dataUpdatePredicate      = []byte{7}
	updatedData              = []byte{0, 12}
)

func TestNewTokenTxSystem(t *testing.T) {
	pdr := types.PartitionDescriptionRecord{
		Version:         1,
		NetworkID:       5,
		PartitionID:     tokens.DefaultPartitionID,
		PartitionTypeID: tokens.PartitionTypeID,
		TypeIDLen:       8,
		UnitIDLen:       256,
		T2Timeout:       2000 * time.Millisecond,
	}
	observe := observability.Default(t)

	t.Run("invalid PartitionID", func(t *testing.T) {
		invalidPDR := pdr
		invalidPDR.PartitionID = 0
		txs, err := NewTxSystem(invalidPDR, observe, WithState(state.NewEmptyState()))
		require.ErrorContains(t, err, `failed to load permissionless fee credit module: invalid fee credit module configuration: invalid PDR: invalid partition identifier: 00000000`)
		require.Nil(t, txs)
	})

	t.Run("state is nil", func(t *testing.T) {
		txs, err := NewTxSystem(pdr, observe, WithState(nil))
		require.EqualError(t, err, "state is nil")
		require.Nil(t, txs)
	})
}

func TestExecuteDefineNFT_WithoutParentID(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: subTypeCreationPredicate,
			TokenMintingPredicate:    tokenMintingPredicate,
			TokenTypeOwnerPredicate:  tokenTypeOwnerPredicate,
			DataUpdatePredicate:      dataUpdatePredicate,
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	//require.NoError(t,tokens.UnitIDGenerator(pdr)(tx,types.ShardID{}))

	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	u, err := txs.State().GetUnit(nftTypeID1, false)
	require.NoError(t, err)
	require.IsType(t, &tokens.NonFungibleTokenTypeData{}, u.Data())
	d := u.Data().(*tokens.NonFungibleTokenTypeData)
	require.Equal(t, zeroSummaryValue, d.SummaryValueInput())
	require.Equal(t, symbol, d.Symbol)
	require.Nil(t, d.ParentTypeID)
	require.EqualValues(t, subTypeCreationPredicate, d.SubTypeCreationPredicate)
	require.EqualValues(t, tokenMintingPredicate, d.TokenMintingPredicate)
	require.EqualValues(t, tokenTypeOwnerPredicate, d.TokenTypeOwnerPredicate)
	require.EqualValues(t, dataUpdatePredicate, d.DataUpdatePredicate)
}

func TestExecuteDefineNFT_WithParentID(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	createParentTx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(parent1Identifier),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: templates.AlwaysTrueBytes(),
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(createParentTx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{createParentTx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)

	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(
			&tokens.DefineNonFungibleTokenAttributes{
				Symbol:                   symbol,
				ParentTypeID:             parent1Identifier,
				SubTypeCreationPredicate: templates.AlwaysFalseBytes(),
			},
		),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{SubTypeCreationProofs: [][]byte{nil}}),
	)
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
}

func TestExecuteDefineNFT_InheritanceChainWithP2PKHPredicates(t *testing.T) {
	// Inheritance Chain: parent1Identifier <- parent2Identifier <- unitIdentifier
	parent2Signer, parent2PubKey := createSigner(t)
	childSigner, childPublicKey := createSigner(t)

	// only parent2 can create subtypes from parent1
	parent1SubTypeCreationPredicate := templates.NewP2pkh256BytesFromKey(parent2PubKey)

	// parent2 and child together can create a subtype because SubTypeCreationPredicate are concatenated (ownerProof must contain both signatures)
	parent2SubTypeCreationPredicate := templates.NewP2pkh256BytesFromKey(childPublicKey)

	txs, _, pdr := newTokenTxSystem(t)

	// create parent1 type
	createParent1Tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(parent1Identifier),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: parent1SubTypeCreationPredicate,
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(createParent1Tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{createParent1Tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	// create parent2 type
	parent2Identifier, err := pdr.ComposeUnitID(pdr.ShardID, tokens.NonFungibleTokenTypeUnitType, tokenid.Random)
	require.NoError(t, err)
	unsignedCreateParent2Tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(parent2Identifier),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(
			&tokens.DefineNonFungibleTokenAttributes{
				Symbol:                   symbol,
				ParentTypeID:             parent1Identifier,
				SubTypeCreationPredicate: parent2SubTypeCreationPredicate,
			},
		),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	_, p2pkhPredicateSig := signTx(t, unsignedCreateParent2Tx, parent2Signer, parent2PubKey)

	signedCreateParent2Tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(parent2Identifier),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(
			&tokens.DefineNonFungibleTokenAttributes{
				Symbol:                   symbol,
				ParentTypeID:             parent1Identifier,
				SubTypeCreationPredicate: parent2SubTypeCreationPredicate,
				//SubTypeCreationProofs: [][]byte{p2pkhPredicateSig},
			},
		),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{SubTypeCreationProofs: [][]byte{p2pkhPredicateSig}}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)

	txr, err = txs.Execute(signedCreateParent2Tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{signedCreateParent2Tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	// create child subtype
	unsignedChildTxAttributes := &tokens.DefineNonFungibleTokenAttributes{
		Symbol:                   symbol,
		ParentTypeID:             parent2Identifier,
		SubTypeCreationPredicate: templates.AlwaysFalseBytes(), // no sub-types
	}
	createChildTx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(
			unsignedChildTxAttributes,
		),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithFeeProof(nil),
	)

	sigBytes, err := createChildTx.AuthProofSigBytes()
	require.NoError(t, err)

	signature, err := childSigner.SignBytes(sigBytes)
	require.NoError(t, err)
	signature2, err := parent2Signer.SignBytes(sigBytes)
	require.NoError(t, err)

	createChildTx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(
			unsignedChildTxAttributes,
		),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{
			SubTypeCreationProofs: [][]byte{
				templates.NewP2pkh256SignatureBytes(signature, childPublicKey), // parent2 p2pkhPredicate argument
				templates.NewP2pkh256SignatureBytes(signature2, parent2PubKey), // parent1 p2pkhPredicate argument
			},
		}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)

	txr, err = txs.Execute(createChildTx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{createChildTx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
}

func TestExecuteDefineNFT_UnitIDIsNil(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nil),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.EqualError(t, err, `invalid transaction: expected 33 byte unit ID, got 0 bytes`)
	require.Nil(t, txr)
}

func TestExecuteDefineNFT_UnitIDHasWrongType(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(tokenid.NewNonFungibleTokenID(t)),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), `transaction validation error (type=2): invalid nft ID: expected type 0X2, got 0X4`)
}

func TestExecuteDefineNFT_ParentTypeIDHasWrongType(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{ParentTypeID: tokenid.NewNonFungibleTokenID(t)}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.EqualError(t, txr.ServerMetadata.ErrDetail(), `transaction validation error (type=2): invalid parent type ID: expected type 0X2, got 0X4`)
}

func TestExecuteDefineNFT_UnitIDExists(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: subTypeCreationPredicate,
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)

	tx.ClientMetadata.Timeout += 1 // increment timeout to pass executed transactions buffer
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), fmt.Sprintf("unit %s exists", nftTypeID1))
}

func TestExecuteDefineNFT_ParentDoesNotExist(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			ParentTypeID:             parent1Identifier,
			SubTypeCreationPredicate: subTypeCreationPredicate,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{SubTypeCreationProofs: [][]byte{templates.EmptyArgument()}}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{SubTypeCreationProofs: [][]byte{templates.EmptyArgument()}}),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), fmt.Sprintf("item %s does not exist", parent1Identifier))
}

func TestExecuteDefineNFT_InvalidParentType(t *testing.T) {
	txs, s, _ := newTokenTxSystem(t)
	require.NoError(t, s.Apply(state.AddUnit(parent1Identifier, &mockUnitData{})))
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			ParentTypeID:             parent1Identifier,
			SubTypeCreationPredicate: subTypeCreationPredicate,
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{SubTypeCreationProofs: [][]byte{{0}}}),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.EqualError(t, txr.ServerMetadata.ErrDetail(), fmt.Sprintf("transaction validation error (type=2): token type SubTypeCreationPredicate: read [0] unit ID %q data: expected unit %[1]v data to be %T got %T", parent1Identifier, &tokens.NonFungibleTokenTypeData{}, &mockUnitData{}))
}

func TestExecuteDefineNFT_InvalidPartitionID(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(0),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{}),
	)
	txr, err := txs.Execute(tx)
	require.EqualError(t, err, "invalid transaction: error invalid partition identifier")
	require.Nil(t, txr)
}

func TestExecuteDefineNFT_InvalidTxType(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{}),
		testtransaction.WithClientMetadata(defaultClientMetadata),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "unknown transaction type")
}

func TestRevertTransaction_Ok(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{ParentTypeID: nil}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.NoError(t, txr.ServerMetadata.ErrDetail())
	txs.Revert()

	_, err = txs.State().GetUnit(nftTypeID1, false)
	require.ErrorContains(t, err, fmt.Sprintf("item %s does not exist", nftTypeID1))
}

func TestExecuteDefineNFT_InvalidSymbolLength(t *testing.T) {
	s := "♥♥ Unicity ♥♥"
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{Symbol: s}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorIs(t, txr.ServerMetadata.ErrDetail(), errInvalidSymbolLength)
}

func TestExecuteDefineNFT_InvalidNameLength(t *testing.T) {
	n := "♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity ♥♥♥♥♥♥♥♥ We ♥ Unicity♥♥"
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithClientMetadata(defaultClientMetadata),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol: symbol,
			Name:   n,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorIs(t, txr.ServerMetadata.ErrDetail(), errInvalidNameLength)
}

func TestExecuteDefineNFT_InvalidIconTypeLength(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithClientMetadata(defaultClientMetadata),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol: symbol,
			Icon:   &tokens.Icon{Type: invalidIconType, Data: []byte{1, 2, 3}},
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorIs(t, txr.ServerMetadata.ErrDetail(), errInvalidIconTypeLength)
}

func TestExecuteDefineNFT_InvalidIconDataLength(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithClientMetadata(defaultClientMetadata),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol: symbol,
			Icon:   &tokens.Icon{Type: validIconType, Data: test.RandomBytes(maxIconDataLength + 1)},
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	require.ErrorIs(t, txr.ServerMetadata.ErrDetail(), errInvalidIconDataLength)
}

func TestMintNFT_Ok(t *testing.T) {
	mintingSigner, mintingVerifier := testsig.CreateSignerAndVerifier(t)
	mintingPublicKey, err := mintingVerifier.MarshalPublicKey()
	require.NoError(t, err)

	txs, _, pdr := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithUnitID(nftTypeID2),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: templates.AlwaysTrueBytes(),
			TokenMintingPredicate:    templates.NewP2pkh256BytesFromKey(mintingPublicKey),
			TokenTypeOwnerPredicate:  templates.AlwaysTrueBytes(),
			DataUpdatePredicate:      templates.AlwaysTrueBytes(),
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)

	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)

	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			OwnerPredicate:      templates.AlwaysTrueBytes(),
			TypeID:              nftTypeID2,
			Name:                nftName,
			URI:                 validNFTURI,
			Data:                []byte{10},
			DataUpdatePredicate: templates.AlwaysTrueBytes(),
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))

	// set minting predicate
	ownerProof := testsig.NewAuthProofSignature(t, tx, mintingSigner)
	authProof := tokens.MintNonFungibleTokenAuthProof{TokenMintingProof: ownerProof}
	require.NoError(t, tx.SetAuthProof(authProof))

	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)

	u, err := txs.State().GetUnit(tx.UnitID, false)
	require.NoError(t, err)
	require.IsType(t, &tokens.NonFungibleTokenData{}, u.Data())

	// verify unit log was added
	unit, err := state.ToUnitV1(u)
	require.NoError(t, err)
	require.Len(t, unit.Logs(), 1)

	d := u.Data().(*tokens.NonFungibleTokenData)
	require.Equal(t, zeroSummaryValue, d.SummaryValueInput())
	require.Equal(t, nftTypeID2, d.TypeID)
	require.Equal(t, nftName, d.Name)
	require.EqualValues(t, []byte{10}, d.Data)
	require.Equal(t, validNFTURI, d.URI)
	require.EqualValues(t, templates.AlwaysTrueBytes(), d.DataUpdatePredicate)
	require.Equal(t, uint64(0), d.Counter)
}

func TestMintNFT_UnitIDIsNil(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithUnitID(nil),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.EqualError(t, err, `invalid transaction: expected 33 byte unit ID, got 0 bytes`)
	require.Nil(t, txr)
}

func TestMintNFT_UnitIDHasWrongType(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	tx := types.TransactionOrder{
		Version: 1,
		Payload: types.Payload{
			NetworkID:      pdr.NetworkID,
			PartitionID:    pdr.PartitionID,
			UnitID:         tokenid.NewFungibleTokenID(t), // FT instead of NFT!
			Type:           tokens.TransactionTypeMintNFT,
			ClientMetadata: createClientMetadata(),
		},
	}
	require.NoError(t, tx.SetAttributes(tokens.MintNonFungibleTokenAttributes{}))
	require.NoError(t, tx.SetAuthProof(tokens.MintNonFungibleTokenAuthProof{}))
	txr, err := txs.Execute(&tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.EqualError(t, txr.ServerMetadata.ErrDetail(), `transaction validation error (type=4): invalid unit ID: expected type 0X4, got 0X3`)
}

func TestMintNFT_AlreadyExists(t *testing.T) {
	txs, s, pdr := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID2),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: templates.AlwaysTrueBytes(),
			TokenMintingPredicate:    templates.AlwaysTrueBytes(),
			TokenTypeOwnerPredicate:  templates.AlwaysTrueBytes(),
			DataUpdatePredicate:      templates.AlwaysTrueBytes(),
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.NoError(t, txr.ServerMetadata.ErrDetail())

	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			OwnerPredicate:      templates.AlwaysTrueBytes(),
			TypeID:              nftTypeID2,
			URI:                 validNFTURI,
			Data:                []byte{10},
			DataUpdatePredicate: templates.AlwaysTrueBytes(),
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{TokenMintingProof: templates.EmptyArgument()}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))

	err = s.Apply(state.AddUnit(tx.UnitID, tokens.NewNonFungibleTokenData(nftTypeID2, &tokens.MintNonFungibleTokenAttributes{})))
	require.NoError(t, err)

	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "token already exists")
}

func TestMintNFT_NameLengthIsInvalid(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			TypeID: nftTypeID1,
			Name:   test.RandomString(maxNameLength + 1),
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorIs(t, txr.ServerMetadata.ErrDetail(), errInvalidNameLength)
}

func TestMintNFT_URILengthIsInvalid(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithClientMetadata(defaultClientMetadata),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			TypeID: nftTypeID1,
			URI:    test.RandomString(4097),
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{}),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "URI exceeds the maximum allowed size of 4096 KB")
}

func TestMintNFT_URIFormatIsInvalid(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			TypeID: nftTypeID1,
			URI:    "invalid_uri",
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "URI invalid_uri is invalid")
}

func TestMintNFT_DataLengthIsInvalid(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)

	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			TypeID: nftTypeID1,
			URI:    validNFTURI,
			Data:   test.RandomBytes(dataMaxSize + 1),
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "data exceeds the maximum allowed size of 65536 KB")
}

func TestMintNFT_NFTTypeDoesNotExist(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)

	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			URI:    validNFTURI,
			Data:   []byte{0, 0, 0, 0},
			TypeID: tokenid.NewNonFungibleTokenTypeID(t),
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{
			TokenMintingProof: []byte{0}},
		),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "nft type does not exist")
}

func TestTransferNFT_UnitDoesNotExist(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)

	nonExistingUnitID := tokenid.NewNonFungibleTokenID(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nonExistingUnitID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			OwnerProof: templates.AlwaysTrueBytes(),
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), fmt.Sprintf("item %s does not exist", nonExistingUnitID))
}

func TestTransferNFT_UnitIsNotNFT(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: subTypeCreationPredicate,
			TokenMintingPredicate:    tokenMintingPredicate,
			TokenTypeOwnerPredicate:  tokenTypeOwnerPredicate,
			DataUpdatePredicate:      dataUpdatePredicate,
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)

	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			OwnerProof: templates.AlwaysTrueBytes(),
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "transaction validation error (type=6): invalid type ID: expected type 0X4, got 0X2")
}

func TestTransferNFT_InvalidCounter(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           1,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{OwnerProof: templates.EmptyArgument()}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "invalid counter")
}

func TestTransferNFT_InvalidTypeID(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            tokenid.NewFungibleTokenTypeID(t),
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			OwnerProof: []byte{0, 0, 0, 1},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "invalid type identifier")
}

func TestTransferNFT_EmptyTypeID(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			TokenTypeOwnerProofs: [][]byte{{0, 0, 0, 1}},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
}

func createClientMetadata() *types.ClientMetadata {
	return &types.ClientMetadata{
		Timeout:           1000,
		MaxTransactionFee: 10,
		FeeCreditRecordID: feeCreditID,
	}
}

func TestTransferNFT_InvalidPredicateFormat(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT from 'always true' to 'p2pkh'
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: test.RandomBytes(32), // invalid owner
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			TokenTypeOwnerProofs: [][]byte{templates.EmptyArgument()},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(templates.EmptyArgument()),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)

	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: templates.NewP2pkh256BytesFromKeyHash(test.RandomBytes(32)),
			Counter:           1,
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(templates.EmptyArgument()),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			OwnerProof:           templates.EmptyArgument(),
			TokenTypeOwnerProofs: nil,
		}),
	)
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "transaction validation error (type=6): evaluating owner predicate: decoding predicate:")
}

func TestTransferNFT_InvalidSignature(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer with invalid signature
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),

		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           0,
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(templates.EmptyArgument()),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			OwnerProof: test.RandomBytes(12),
			// the NFT we transfer has "always true" bearer predicate so providing
			// arguments for it makes it fail
			TokenTypeOwnerProofs: [][]byte{{0x0B, 0x0A, 0x0D}},
		}),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.EqualError(t, txr.ServerMetadata.ErrDetail(), `transaction validation error (type=6): evaluating owner predicate: executing predicate: "always true" predicate arguments must be empty`)
}

func TestTransferNFT_Ok(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: templates.AlwaysTrueBytes(),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			TokenTypeOwnerProofs: [][]byte{templates.EmptyArgument()},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)

	u, err := txs.State().GetUnit(nftID, false)
	require.NoError(t, err)
	require.IsType(t, &tokens.NonFungibleTokenData{}, u.Data())
	d := u.Data().(*tokens.NonFungibleTokenData)
	require.Equal(t, zeroSummaryValue, d.SummaryValueInput())
	require.Equal(t, nftTypeID2, d.TypeID)
	require.Equal(t, nftName, d.Name)
	require.EqualValues(t, []byte{10}, d.Data)
	require.Equal(t, validNFTURI, d.URI)
	require.EqualValues(t, templates.AlwaysTrueBytes(), d.DataUpdatePredicate)
	require.Equal(t, uint64(1), d.Counter)
	require.EqualValues(t, templates.AlwaysTrueBytes(), d.Owner())
}

func TestTransferNFT_BurnedBearerMustFail(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// transfer NFT, set bearer to un-spendable predicate
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: templates.AlwaysFalseBytes(),
			Counter:           0,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			TokenTypeOwnerProofs: [][]byte{templates.EmptyArgument()},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)

	u, err := txs.State().GetUnit(nftID, false)
	require.NoError(t, err)
	require.IsType(t, &tokens.NonFungibleTokenData{}, u.Data())
	require.EqualValues(t, templates.AlwaysFalseBytes(), u.Data().Owner())

	// the token must be considered as burned and not transferable
	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeTransferNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),

		testtransaction.WithAttributes(&tokens.TransferNonFungibleTokenAttributes{
			TypeID:            nftTypeID2,
			NewOwnerPredicate: templates.AlwaysFalseBytes(),
			Counter:           1,
		}),
		testtransaction.WithAuthProof(&tokens.TransferNonFungibleTokenAuthProof{
			TokenTypeOwnerProofs: [][]byte{templates.EmptyArgument()},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "evaluating owner predicate: predicate evaluated to \"false\"")
}

func TestUpdateNFT_DataLengthIsInvalid(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Data:    test.RandomBytes(dataMaxSize + 1),
			Counter: 0,
		}),
		testtransaction.WithAuthProof(&tokens.UpdateNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "data exceeds the maximum allowed size of 65536 KB")
}

func TestUpdateNFT_UnitDoesNotExist(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftUnitID, err := pdr.ComposeUnitID(types.ShardID{}, tokens.NonFungibleTokenUnitType, func(b []byte) error { return nil })
	require.NoError(t, err)

	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(nftUnitID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Data:    test.RandomBytes(0),
			Counter: 0,
		}),
		testtransaction.WithAuthProof(&tokens.UpdateNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), fmt.Sprintf("item %s does not exist", nftUnitID))
}

func TestUpdateNFT_UnitIsNotNFT(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t)
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: subTypeCreationPredicate,
			TokenMintingPredicate:    tokenMintingPredicate,
			TokenTypeOwnerPredicate:  tokenTypeOwnerPredicate,
			DataUpdatePredicate:      dataUpdatePredicate,
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)

	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(nftTypeID1),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Data:    test.RandomBytes(10),
			Counter: 0,
		}),
		testtransaction.WithAuthProof(&tokens.UpdateNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "invalid unit ID")
}

func TestUpdateNFT_InvalidCounter(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Data:    test.RandomBytes(10),
			Counter: 1,
		}),
		testtransaction.WithAuthProof(&tokens.UpdateNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.ErrorContains(t, txr.ServerMetadata.ErrDetail(), "invalid counter")
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
}

func TestUpdateNFT_InvalidSignature(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)

	// create NFT type
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID2),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: templates.AlwaysTrueBytes(),
			TokenMintingPredicate:    templates.AlwaysTrueBytes(),
			TokenTypeOwnerPredicate:  templates.AlwaysTrueBytes(),
			DataUpdatePredicate:      templates.AlwaysTrueBytes(),
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(&tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(&types.ClientMetadata{
			Timeout:           1000,
			MaxTransactionFee: 10,
			FeeCreditRecordID: feeCreditID,
		}),
		testtransaction.WithFeeProof(nil),
	)

	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.EqualValues(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.NoError(t, txr.ServerMetadata.ErrDetail())

	// mint NFT
	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			OwnerPredicate:      templates.AlwaysTrueBytes(),
			TypeID:              nftTypeID2,
			Name:                nftName,
			URI:                 validNFTURI,
			Data:                []byte{10},
			DataUpdatePredicate: templates.NewP2pkh256BytesFromKeyHash(test.RandomBytes(32)),
		}),
		testtransaction.WithAuthProof(&tokens.MintNonFungibleTokenAuthProof{
			TokenMintingProof: templates.EmptyArgument()},
		),
		testtransaction.WithClientMetadata(&types.ClientMetadata{
			Timeout:           1000,
			MaxTransactionFee: 10,
			FeeCreditRecordID: feeCreditID,
		}),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, &pdr))
	nftID := tx.UnitID

	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.NoError(t, txr.ServerMetadata.ErrDetail())
	require.EqualValues(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())

	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Data:    test.RandomBytes(10),
			Counter: 0,
		}),
		testtransaction.WithAuthProof(&tokens.UpdateNonFungibleTokenAuthProof{
			// the previous mint tx did set the DataUpdatePredicate to p2pkh so for the tx to be valid
			// the first argument here should be CBOR of pubkey and signature pair
			TokenDataUpdateProof:      []byte{0},
			TokenTypeDataUpdateProofs: [][]byte{templates.EmptyArgument()},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{feeCreditID}, txr.TargetUnits())
	require.EqualError(t, txr.ServerMetadata.ErrDetail(), `transaction validation error (type=10): data update predicate: executing predicate: failed to decode P2PKH256 signature: cbor: cannot unmarshal positive integer into Go value of type templates.P2pkh256Signature`)
}

func TestUpdateNFT_Ok(t *testing.T) {
	txs, _, pdr := newTokenTxSystem(t)
	nftID := defineNFTAndMintToken(t, txs, &pdr, nftTypeID2)

	// update NFT
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeUpdateNFT),
		testtransaction.WithUnitID(nftID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.UpdateNonFungibleTokenAttributes{
			Counter: 0,
			Data:    updatedData,
			//DataUpdateSignatures: [][]byte{nil, nil},
		}),
		testtransaction.WithAuthProof(&tokens.UpdateNonFungibleTokenAuthProof{
			TokenDataUpdateProof:      templates.EmptyArgument(),
			TokenTypeDataUpdateProofs: [][]byte{templates.EmptyArgument()},
		}),
		testtransaction.WithClientMetadata(createClientMetadata()),
		testtransaction.WithFeeProof(nil),
	)
	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{nftID, feeCreditID}, txr.TargetUnits())

	u, err := txs.State().GetUnit(nftID, false)
	require.NoError(t, err)
	require.IsType(t, &tokens.NonFungibleTokenData{}, u.Data())
	d := u.Data().(*tokens.NonFungibleTokenData)
	require.Equal(t, zeroSummaryValue, d.SummaryValueInput())
	require.Equal(t, nftTypeID2, d.TypeID)
	require.Equal(t, nftName, d.Name)
	require.EqualValues(t, updatedData, d.Data)
	require.Equal(t, validNFTURI, d.URI)
	require.EqualValues(t, templates.AlwaysTrueBytes(), d.DataUpdatePredicate)
	require.Equal(t, uint64(1), d.Counter)
	require.EqualValues(t, templates.AlwaysTrueBytes(), d.Owner())
}

func TestExecute_FailedTxInFeelessMode(t *testing.T) {
	txs, _, _ := newTokenTxSystem(t,
		WithAdminOwnerPredicate(templates.AlwaysTrueBytes()),
		WithFeelessMode(true))

	// add fee credit record (not supported in feeless mode)
	signer, _ := testsig.CreateSignerAndVerifier(t)
	addFC := testutils.NewAddFC(t, signer, nil,
		testtransaction.WithUnitID(feeCreditID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithClientMetadata(createClientMetadata()),
	)

	// Failed tx in feeless mode does not change state
	ss, err := txs.StateSummary()
	require.NoError(t, err)
	rootHashBefore := ss.Root()

	u, err := txs.State().GetUnit(feeCreditID, false)
	require.NoError(t, err)
	fcrBefore, ok := u.Data().(*fc.FeeCreditRecord)
	require.True(t, ok)

	txr, err := txs.Execute(addFC)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusFailed, txr.ServerMetadata.SuccessIndicator)
	require.EqualValues(t, 0, txr.ServerMetadata.ActualFee)

	u, err = txs.State().GetUnit(feeCreditID, false)
	require.NoError(t, err)
	fcrAfter, ok := u.Data().(*fc.FeeCreditRecord)
	require.True(t, ok)
	require.Equal(t, fcrBefore.Balance, fcrAfter.Balance)

	ss, err = txs.EndBlock()
	require.NoError(t, err)
	require.Equal(t, rootHashBefore, ss.Root())
}

func defineNFTAndMintToken(t *testing.T, txs *txsystem.GenericTxSystem, pdr *types.PartitionDescriptionRecord, nftTypeID types.UnitID) types.UnitID {
	// define NFT type
	tx := testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeDefineNFT),
		testtransaction.WithUnitID(nftTypeID),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.DefineNonFungibleTokenAttributes{
			Symbol:                   symbol,
			SubTypeCreationPredicate: templates.AlwaysTrueBytes(),
			TokenMintingPredicate:    templates.AlwaysTrueBytes(),
			TokenTypeOwnerPredicate:  templates.AlwaysTrueBytes(),
			DataUpdatePredicate:      templates.AlwaysTrueBytes(),
			ParentTypeID:             nil,
		}),
		testtransaction.WithAuthProof(tokens.DefineNonFungibleTokenAuthProof{}),
		testtransaction.WithClientMetadata(&types.ClientMetadata{
			Timeout:           1000,
			MaxTransactionFee: 10,
			FeeCreditRecordID: feeCreditID,
		}),
		testtransaction.WithFeeProof(nil),
	)

	txr, err := txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)

	// mint NFT
	tx = testtransaction.NewTransactionOrder(
		t,
		testtransaction.WithTransactionType(tokens.TransactionTypeMintNFT),
		testtransaction.WithPartitionID(tokens.DefaultPartitionID),
		testtransaction.WithAttributes(&tokens.MintNonFungibleTokenAttributes{
			OwnerPredicate:      templates.AlwaysTrueBytes(),
			TypeID:              nftTypeID,
			Name:                nftName,
			URI:                 validNFTURI,
			Data:                []byte{10},
			DataUpdatePredicate: templates.AlwaysTrueBytes(),
		}),
		testtransaction.WithAuthProof(tokens.MintNonFungibleTokenAuthProof{TokenMintingProof: templates.EmptyArgument()}),
		testtransaction.WithClientMetadata(&types.ClientMetadata{
			Timeout:           1000,
			MaxTransactionFee: 10,
			FeeCreditRecordID: feeCreditID,
		}),
		testtransaction.WithFeeProof(nil),
	)
	require.NoError(t, tokens.GenerateUnitID(tx, pdr))
	txr, err = txs.Execute(tx)
	require.NoError(t, err)
	require.NotNil(t, txr)
	require.Equal(t, types.TxStatusSuccessful, txr.ServerMetadata.SuccessIndicator)
	require.Equal(t, []types.UnitID{tx.UnitID, feeCreditID}, txr.TargetUnits())
	require.True(t, txr.ServerMetadata.ActualFee > 0)
	return tx.UnitID
}

type mockUnitData struct{}

func (m mockUnitData) Write(hasher abhash.Hasher) { hasher.Write(&m) }

func (m mockUnitData) SummaryValueInput() uint64 {
	return 0
}

func (m mockUnitData) Copy() types.UnitData {
	return &mockUnitData{}
}

func (m mockUnitData) Owner() []byte {
	return nil
}

func (m mockUnitData) GetVersion() types.Version {
	return 0
}

func createSigner(t *testing.T) (abcrypto.Signer, []byte) {
	t.Helper()
	signer, err := abcrypto.NewInMemorySecp256K1Signer()
	require.NoError(t, err)

	verifier, err := signer.Verifier()
	require.NoError(t, err)

	pubKey, err := verifier.MarshalPublicKey()
	require.NoError(t, err)
	return signer, pubKey
}

func signTx(t *testing.T, tx *types.TransactionOrder, signer abcrypto.Signer, pubKey []byte) ([]byte, []byte) {
	sigBytes, err := tx.AuthProofSigBytes()
	require.NoError(t, err)
	signature, err := signer.SignBytes(sigBytes)
	require.NoError(t, err)
	return signature, templates.NewP2pkh256SignatureBytes(signature, pubKey)
}

func newTokenTxSystem(t *testing.T, opts ...Option) (*txsystem.GenericTxSystem, *state.State, types.PartitionDescriptionRecord) {
	_, verifier := testsig.CreateSignerAndVerifier(t)
	s := state.NewEmptyState()
	require.NoError(t, s.Apply(state.AddUnit(feeCreditID, &fc.FeeCreditRecord{
		Balance:        100,
		OwnerPredicate: templates.AlwaysTrueBytes(),
		Counter:        10,
		MinLifetime:    1000,
	})))
	summaryValue, summaryHash, err := s.CalculateRoot()
	require.NoError(t, err)
	require.NoError(t, s.Commit(&types.UnicityCertificate{Version: 1, InputRecord: &types.InputRecord{
		Version:      1,
		RoundNumber:  1,
		Hash:         summaryHash,
		SummaryValue: util.Uint64ToBytes(summaryValue),
	}}))
	pdr := types.PartitionDescriptionRecord{
		Version:         1,
		NetworkID:       5,
		PartitionID:     tokens.DefaultPartitionID,
		PartitionTypeID: tokens.PartitionTypeID,
		TypeIDLen:       8,
		UnitIDLen:       256,
		T2Timeout:       2000 * time.Millisecond,
	}

	opts = append(opts, WithTrustBase(testtb.NewTrustBase(t, verifier)), WithState(s))
	txs, err := NewTxSystem(
		pdr,
		observability.Default(t),
		opts...,
	)
	require.NoError(t, err)
	return txs, s, pdr
}
