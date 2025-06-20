package tokenenc

import (
	"errors"

	"github.com/unicitynetwork/bft-core/predicates/wasm/wvm/encoder"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
)

func RegisterUnitDataEncoders(reg func(ud any, enc encoder.UnitDataEncoder) error) error {
	return errors.Join(
		reg(&tokens.NonFungibleTokenData{}, udeNonFungibleTokenData),
		reg(&tokens.NonFungibleTokenTypeData{}, udeNonFungibleTokenTypeData),
		reg(&tokens.FungibleTokenTypeData{}, udeFungibleTokenTypeData),
		reg(&tokens.FungibleTokenData{}, udeFungibleTokenData),
	)
}

func udeNonFungibleTokenData(data types.UnitData, ver uint32) ([]byte, error) {
	value := data.(*tokens.NonFungibleTokenData)
	buf := encoder.TVEnc{}
	buf.EncodeTagged(1, value.TypeID)
	if value.Name != "" {
		buf.EncodeTagged(2, value.Name)
	}
	if value.URI != "" {
		buf.EncodeTagged(3, value.URI)
	}
	if value.Data != nil {
		buf.EncodeTagged(4, value.Data)
	}
	buf.EncodeTagged(5, value.Counter)
	return buf.Bytes()
}

func udeNonFungibleTokenTypeData(data types.UnitData, ver uint32) ([]byte, error) {
	value := data.(*tokens.NonFungibleTokenTypeData)
	buf := encoder.TVEnc{}
	if len(value.ParentTypeID) != 0 {
		buf.EncodeTagged(1, value.ParentTypeID)
	}
	buf.EncodeTagged(2, value.Symbol)
	buf.EncodeTagged(3, value.Name)
	return buf.Bytes()
}

func udeFungibleTokenTypeData(data types.UnitData, ver uint32) ([]byte, error) {
	value := data.(*tokens.FungibleTokenTypeData)
	buf := encoder.TVEnc{}
	if len(value.ParentTypeID) != 0 {
		buf.EncodeTagged(1, value.ParentTypeID)
	}
	buf.EncodeTagged(2, value.Symbol)
	buf.EncodeTagged(3, value.Name)
	buf.EncodeTagged(4, value.DecimalPlaces)
	return buf.Bytes()
}

func udeFungibleTokenData(data types.UnitData, ver uint32) ([]byte, error) {
	value := data.(*tokens.FungibleTokenData)
	buf := encoder.TVEnc{}
	buf.EncodeTagged(1, value.TypeID)
	buf.EncodeTagged(2, value.Value)
	buf.EncodeTagged(3, value.Counter)
	buf.EncodeTagged(4, value.MinLifetime)
	return buf.Bytes()
}
