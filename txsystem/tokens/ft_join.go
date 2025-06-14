package tokens

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/unicitynetwork/bft-core/state"
	txtypes "github.com/unicitynetwork/bft-core/txsystem/types"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
	"github.com/unicitynetwork/bft-go-base/util"
)

func (m *FungibleTokensModule) executeJoinFT(tx *types.TransactionOrder, _ *tokens.JoinFungibleTokenAttributes, _ *tokens.JoinFungibleTokenAuthProof, exeCtx txtypes.ExecutionContext) (*types.ServerMetadata, error) {
	unitID := tx.GetUnitID()
	sum := util.BytesToUint64(exeCtx.GetData())
	// update state
	if err := m.state.Apply(
		state.UpdateUnitData(unitID,
			func(data types.UnitData) (types.UnitData, error) {
				tokenData, ok := data.(*tokens.FungibleTokenData)
				if !ok {
					return nil, fmt.Errorf("unit %v does not contain fungible token data", unitID)
				}
				tokenData.Value = sum
				tokenData.Counter += 1
				return tokenData, nil
			},
		),
	); err != nil {
		return nil, err
	}
	return &types.ServerMetadata{TargetUnits: []types.UnitID{unitID}, SuccessIndicator: types.TxStatusSuccessful}, nil
}

func (m *FungibleTokensModule) validateJoinFT(tx *types.TransactionOrder, attr *tokens.JoinFungibleTokenAttributes, authProof *tokens.JoinFungibleTokenAuthProof, exeCtx txtypes.ExecutionContext) error {
	if tx.HasStateLock() {
		return errors.New("joinFT transaction cannot contain state lock")
	}
	tokenData, err := m.getFungibleTokenData(tx.UnitID)
	if err != nil {
		return err
	}
	sum := tokenData.Value
	var prevUnitID types.UnitID
	for i, btx := range attr.BurnTokenProofs {
		burnTxo, err := btx.GetTransactionOrderV1()
		if err != nil {
			return fmt.Errorf("failed to get burn transaction order: %w", err)
		}
		btxAttr := &tokens.BurnFungibleTokenAttributes{}
		if err := burnTxo.UnmarshalAttributes(btxAttr); err != nil {
			return fmt.Errorf("failed to unmarshal burn fungible token attributes")
		}

		var ok bool
		sum, ok = util.SafeAdd(sum, btxAttr.Value)
		if !ok {
			return errors.New("invalid sum of tokens: uint64 overflow")
		}

		if i > 0 && burnTxo.UnitID.Compare(prevUnitID) != 1 {
			// burning transactions orders are listed in strictly increasing order of token identifiers
			// this ensures that no source token can be included multiple times
			return errors.New("burn transaction orders are not listed in strictly increasing order of token identifiers")
		}
		if !bytes.Equal(btxAttr.TypeID, tokenData.TypeID) {
			return fmt.Errorf("the type of the burned source token does not match the type of target token: expected %s, got %s", tokenData.TypeID, btxAttr.TypeID)
		}
		if burnTxo.NetworkID != tx.NetworkID {
			return fmt.Errorf("burn transaction network id does not match with join transaction network id: burn transaction %d join transaction %d", burnTxo.NetworkID, tx.NetworkID)
		}
		if burnTxo.PartitionID != tx.PartitionID {
			return fmt.Errorf("burn transaction partition id does not match with join transaction partition id: burn transaction %d, join transaction %d", burnTxo.PartitionID, tx.PartitionID)
		}
		if !bytes.Equal(btxAttr.TargetTokenID, tx.UnitID) {
			return fmt.Errorf("burn transaction target token id does not match with join transaction unit id: burn transaction %s, join transaction %s", btxAttr.TargetTokenID, tx.UnitID)
		}
		if btxAttr.TargetTokenCounter != tokenData.Counter {
			return fmt.Errorf("burn transaction target token counter does not match with target unit counter: burn transaction counter %d, unit counter %d", btxAttr.TargetTokenCounter, tokenData.Counter)
		}
		if err = types.VerifyTxProof(btx, m.trustBase, m.hashAlgorithm); err != nil {
			return fmt.Errorf("proof is not valid: %w", err)
		}
		prevUnitID = burnTxo.UnitID
	}

	exeCtx = exeCtx.WithExArg(tx.AuthProofSigBytes)
	if err = m.execPredicate(tokenData.Owner(), authProof.OwnerProof, tx, exeCtx); err != nil {
		return fmt.Errorf("evaluating owner predicate: %w", err)
	}
	err = runChainedPredicates[*tokens.FungibleTokenTypeData](
		exeCtx,
		tx,
		tokenData.TypeID,
		authProof.TokenTypeOwnerProofs,
		m.execPredicate,
		func(d *tokens.FungibleTokenTypeData) (types.UnitID, []byte) {
			return d.ParentTypeID, d.TokenTypeOwnerPredicate
		},
		m.state.GetUnit,
	)
	if err != nil {
		return fmt.Errorf("evaluating TokenTypeOwnerPredicate: %w", err)
	}

	// add sum of token values to tx context
	exeCtx.SetData(util.Uint64ToBytes(sum))

	return nil
}
