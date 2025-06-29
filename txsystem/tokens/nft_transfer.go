package tokens

import (
	"bytes"
	"fmt"

	"github.com/unicitynetwork/bft-core/state"
	txtypes "github.com/unicitynetwork/bft-core/txsystem/types"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
)

func (n *NonFungibleTokensModule) executeTransferNFT(tx *types.TransactionOrder, attr *tokens.TransferNonFungibleTokenAttributes, _ *tokens.TransferNonFungibleTokenAuthProof, _ txtypes.ExecutionContext) (*types.ServerMetadata, error) {
	unitID := tx.GetUnitID()

	// 1. N[T.ι].D.φ ← T.A.φ
	// 2. N[T.ι].D.c ← N[T.ι].D.c + 1
	if err := n.state.Apply(
		state.UpdateUnitData(unitID, func(data types.UnitData) (types.UnitData, error) {
			d, ok := data.(*tokens.NonFungibleTokenData)
			if !ok {
				return nil, fmt.Errorf("unit %v does not contain non fungible token data", unitID)
			}
			d.OwnerPredicate = attr.NewOwnerPredicate
			d.Counter += 1
			return d, nil
		}),
	); err != nil {
		return nil, err
	}
	return &types.ServerMetadata{TargetUnits: []types.UnitID{unitID}, SuccessIndicator: types.TxStatusSuccessful}, nil
}

func (n *NonFungibleTokensModule) validateTransferNFT(tx *types.TransactionOrder, attr *tokens.TransferNonFungibleTokenAttributes, authProof *tokens.TransferNonFungibleTokenAuthProof, exeCtx txtypes.ExecutionContext) error {
	unitID := tx.GetUnitID()
	if err := unitID.TypeMustBe(tokens.NonFungibleTokenUnitType, &n.pdr); err != nil {
		return fmt.Errorf("invalid type ID: %w", err)
	}
	u, err := n.state.GetUnit(unitID, false)
	if err != nil {
		return err
	}
	data, ok := u.Data().(*tokens.NonFungibleTokenData)
	if !ok {
		return fmt.Errorf("validate nft transfer: unit %v is not a non-fungible token type", unitID)
	}
	if data.Counter != attr.Counter {
		return fmt.Errorf("invalid counter: expected %d, got %d", data.Counter, attr.Counter)
	}
	tokenTypeID := data.TypeID
	if !bytes.Equal(attr.TypeID, tokenTypeID) {
		return fmt.Errorf("invalid type identifier: expected '%s', got '%s'", tokenTypeID, attr.TypeID)
	}

	exeCtx = exeCtx.WithExArg(tx.AuthProofSigBytes)
	if err = n.execPredicate(data.OwnerPredicate, authProof.OwnerProof, tx, exeCtx); err != nil {
		return fmt.Errorf("evaluating owner predicate: %w", err)
	}
	err = runChainedPredicates[*tokens.NonFungibleTokenTypeData](
		exeCtx,
		tx,
		tokenTypeID,
		authProof.TokenTypeOwnerProofs,
		n.execPredicate,
		func(d *tokens.NonFungibleTokenTypeData) (types.UnitID, []byte) {
			return d.ParentTypeID, d.TokenTypeOwnerPredicate
		},
		n.state.GetUnit,
	)
	if err != nil {
		return fmt.Errorf("token type owner predicate: %w", err)
	}
	return nil
}
