package tokens

import (
	"errors"
	"fmt"

	"github.com/unicitynetwork/bft-core/state"
	"github.com/unicitynetwork/bft-core/tree/avl"
	txtypes "github.com/unicitynetwork/bft-core/txsystem/types"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
)

func (m *FungibleTokensModule) executeDefineFT(tx *types.TransactionOrder, attr *tokens.DefineFungibleTokenAttributes, _ *tokens.DefineFungibleTokenAuthProof, _ txtypes.ExecutionContext) (*types.ServerMetadata, error) {
	unitID := tx.GetUnitID()

	if err := m.state.Apply(
		state.AddUnit(unitID, tokens.NewFungibleTokenTypeData(attr)),
	); err != nil {
		return nil, err
	}

	return &types.ServerMetadata{TargetUnits: []types.UnitID{unitID}, SuccessIndicator: types.TxStatusSuccessful}, nil
}

func (m *FungibleTokensModule) validateDefineFT(tx *types.TransactionOrder, attr *tokens.DefineFungibleTokenAttributes, authProof *tokens.DefineFungibleTokenAuthProof, exeCtx txtypes.ExecutionContext) error {
	if tx.HasStateLock() {
		return errors.New("defFT transaction cannot contain state lock")
	}
	unitID := tx.GetUnitID()
	if err := unitID.TypeMustBe(tokens.FungibleTokenTypeUnitType, &m.pdr); err != nil {
		return fmt.Errorf("invalid unit ID: %w", err)
	}
	if attr.ParentTypeID != nil {
		if err := attr.ParentTypeID.TypeMustBe(tokens.FungibleTokenTypeUnitType, &m.pdr); err != nil {
			return fmt.Errorf("invalid parent type: %w", err)
		}
	}
	if len(attr.Symbol) > maxSymbolLength {
		return errInvalidSymbolLength
	}
	if len(attr.Name) > maxNameLength {
		return errInvalidNameLength
	}
	if attr.Icon != nil {
		if len(attr.Icon.Type) > maxIconTypeLength {
			return errInvalidIconTypeLength
		}
		if len(attr.Icon.Data) > maxIconDataLength {
			return errInvalidIconDataLength
		}
	}

	decimalPlaces := attr.DecimalPlaces
	if decimalPlaces > maxDecimalPlaces {
		return fmt.Errorf("invalid decimal places. maximum allowed value %v, got %v", maxDecimalPlaces, decimalPlaces)
	}

	u, err := m.state.GetUnit(unitID, false)
	if u != nil {
		return fmt.Errorf("unit %v exists", unitID)
	}
	if !errors.Is(err, avl.ErrNotFound) {
		return err
	}

	if attr.ParentTypeID != nil {
		parentData, err := getUnitData[*tokens.FungibleTokenTypeData](m.state.GetUnit, attr.ParentTypeID)
		if err != nil {
			return err
		}
		if decimalPlaces != parentData.DecimalPlaces {
			return fmt.Errorf("invalid decimal places. allowed %v, got %v", parentData.DecimalPlaces, decimalPlaces)
		}
	}

	err = runChainedPredicates[*tokens.FungibleTokenTypeData](
		exeCtx.WithExArg(tx.AuthProofSigBytes),
		tx,
		attr.ParentTypeID,
		authProof.SubTypeCreationProofs,
		m.execPredicate,
		func(d *tokens.FungibleTokenTypeData) (types.UnitID, []byte) {
			return d.ParentTypeID, d.SubTypeCreationPredicate
		},
		m.state.GetUnit,
	)
	if err != nil {
		return fmt.Errorf("SubTypeCreationPredicate: %w", err)
	}
	return nil
}
