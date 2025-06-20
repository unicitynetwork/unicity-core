package rpc

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/require"
	test "github.com/unicitynetwork/bft-core/internal/testutils"
	testobservability "github.com/unicitynetwork/bft-core/internal/testutils/observability"
	testsig "github.com/unicitynetwork/bft-core/internal/testutils/sig"
	"github.com/unicitynetwork/bft-core/internal/testutils/trustbase"
	testtxsystem "github.com/unicitynetwork/bft-core/internal/testutils/txsystem"
	"github.com/unicitynetwork/bft-core/network"
	"github.com/unicitynetwork/bft-core/partition"
	"github.com/unicitynetwork/bft-core/state"
	"github.com/unicitynetwork/bft-core/txsystem"
	abhash "github.com/unicitynetwork/bft-go-base/hash"
	"github.com/unicitynetwork/bft-go-base/predicates/templates"
	"github.com/unicitynetwork/bft-go-base/txsystem/money"
	"github.com/unicitynetwork/bft-go-base/txsystem/tokens"
	"github.com/unicitynetwork/bft-go-base/types"
	"github.com/unicitynetwork/bft-go-base/types/hex"
	"github.com/unicitynetwork/bft-go-base/util"
)

func TestGetRoundInfo(t *testing.T) {
	observe := testobservability.Default(t)
	node := &MockNode{}
	api := NewStateAPI(node, observe)

	t.Run("ok", func(t *testing.T) {
		node.maxRoundNumber = 1337
		node.currentEpochNumber = 1234

		roundInfo, err := api.GetRoundInfo(context.Background())
		require.NoError(t, err)
		require.EqualValues(t, 1337, roundInfo.RoundNumber)
		require.EqualValues(t, 1234, roundInfo.EpochNumber)
	})
	t.Run("err", func(t *testing.T) {
		node.err = errors.New("some error")
		node.maxRoundNumber = 1337
		node.currentEpochNumber = 1234

		roundInfo, err := api.GetRoundInfo(context.Background())
		require.ErrorContains(t, err, "some error")
		require.Nil(t, roundInfo)
	})
}

func TestGetUnit(t *testing.T) {
	observe := testobservability.Default(t)
	unitID := test.RandomBytes(33)
	node := &MockNode{
		txs: &testtxsystem.CounterTxSystem{
			FixedState: prepareState(t, unitID),
		},
	}
	api := NewStateAPI(node, observe)

	t.Run("get unit (proof=false)", func(t *testing.T) {
		unit, err := api.GetUnit(unitID, false)
		require.NoError(t, err)
		require.NotNil(t, unit)
		require.NotNil(t, unit.Data)
		d, ok := unit.Data.(*unitData)
		require.True(t, ok)
		require.Nil(t, unit.StateProof)
		require.EqualValues(t, templates.AlwaysTrueBytes(), d.O)
	})
	t.Run("get unit (proof=true)", func(t *testing.T) {
		unit, err := api.GetUnit(unitID, true)
		require.NoError(t, err)
		require.NotNil(t, unit)
		require.NotNil(t, unit.Data)
		require.NotNil(t, unit.StateProof)
		require.EqualValues(t, unitID, unit.StateProof.UnitID)
	})
	t.Run("unit not found", func(t *testing.T) {
		unit, err := api.GetUnit([]byte{1, 2, 3}, false)
		require.NoError(t, err)
		require.Nil(t, unit)
	})
	t.Run("network and partition identifier exist", func(t *testing.T) {
		unit, err := api.GetUnit(unitID, false)
		require.NoError(t, err)
		require.NotNil(t, unit)
		require.Equal(t, types.NetworkID(5), unit.NetworkID)
		require.Equal(t, types.PartitionID(0x00010000), unit.PartitionID)
	})
	t.Run("stateLockTx exists", func(t *testing.T) {
		stateLockTx := []byte{1}

		s := state.NewEmptyState()
		require.NoError(t, s.Apply(
			state.AddUnit(unitID, &unitData{I: 10, O: templates.AlwaysTrueBytes()}),
		))
		require.NoError(t, s.Apply(state.SetStateLock(unitID, stateLockTx)))
		require.NoError(t, s.AddUnitLog(unitID, test.RandomBytes(32)))

		summaryValue, summaryHash, err := s.CalculateRoot()
		require.NoError(t, err)
		require.NoError(t, s.Commit(&types.UnicityCertificate{Version: 1, InputRecord: &types.InputRecord{
			Version:      1,
			RoundNumber:  1,
			Hash:         summaryHash,
			SummaryValue: util.Uint64ToBytes(summaryValue),
		}}))

		node := &MockNode{
			txs: &testtxsystem.CounterTxSystem{
				FixedState: s,
			},
		}
		api := NewStateAPI(node, observe)

		unit, err := api.GetUnit(unitID, false)
		require.NoError(t, err)
		require.NotNil(t, unit)
		require.EqualValues(t, stateLockTx, unit.StateLockTx)
	})
}

func TestGetUnitsByOwnerID(t *testing.T) {
	observe := testobservability.Default(t)
	node := &MockNode{}
	ownerIndex := &MockOwnerIndex{ownerUnits: map[string][]types.UnitID{}}
	api := NewStateAPI(node, observe, WithOwnerIndex(ownerIndex))

	t.Run("ok", func(t *testing.T) {
		ownerID := []byte{1}
		ownerIndex.ownerUnits[string(ownerID)] = []types.UnitID{[]byte{0}, []byte{1}}

		unitIds, err := api.GetUnitsByOwnerID(ownerID, nil, nil)
		require.NoError(t, err)
		require.Len(t, unitIds, 2)
		require.EqualValues(t, []byte{0}, unitIds[0])
		require.EqualValues(t, []byte{1}, unitIds[1])
	})
	t.Run("err", func(t *testing.T) {
		ownerID := []byte{1}
		ownerIndex.err = errors.New("some error")

		unitIds, err := api.GetUnitsByOwnerID(ownerID, nil, nil)
		require.ErrorContains(t, err, "some error")
		require.Nil(t, unitIds)
		ownerIndex.err = nil
	})
	t.Run("pagination", func(t *testing.T) {
		ownerID := []byte{1}
		ownerIndex.ownerUnits[string(ownerID)] = []types.UnitID{[]byte{3}, []byte{1}, []byte{2}, []byte{0}, []byte{4}}

		limit := 2
		unitIds, err := api.GetUnitsByOwnerID(ownerID, nil, &limit)
		require.NoError(t, err)
		require.Len(t, unitIds, 2)
		require.EqualValues(t, []byte{3}, unitIds[0])
		require.EqualValues(t, []byte{1}, unitIds[1])

		unitIds, err = api.GetUnitsByOwnerID(ownerID, &unitIds[1], &limit)
		require.NoError(t, err)
		require.Len(t, unitIds, 2)
		require.EqualValues(t, []byte{2}, unitIds[0])
		require.EqualValues(t, []byte{0}, unitIds[1])

		unitIds, err = api.GetUnitsByOwnerID(ownerID, &unitIds[1], &limit)
		require.NoError(t, err)
		require.Len(t, unitIds, 1)
		require.EqualValues(t, []byte{4}, unitIds[0])

		unitIds, err = api.GetUnitsByOwnerID(ownerID, &unitIds[0], &limit)
		require.NoError(t, err)
		require.Len(t, unitIds, 0)
	})
	t.Run("limit", func(t *testing.T) {
		ownerID := []byte{1}
		ownerIndex.ownerUnits[string(ownerID)] = []types.UnitID{[]byte{0}, []byte{1}}
		apiWithLimit := NewStateAPI(node, observe, WithOwnerIndex(ownerIndex), WithResponseItemLimit(1))

		unitIds, err := apiWithLimit.GetUnitsByOwnerID(ownerID, nil, nil)
		require.NoError(t, err)
		require.Len(t, unitIds, 1)
		require.EqualValues(t, []byte{0}, unitIds[0])

		limit := 2
		unitIds, err = apiWithLimit.GetUnitsByOwnerID(ownerID, nil, &limit)
		require.NoError(t, err)
		require.Len(t, unitIds, 1)
		require.EqualValues(t, []byte{0}, unitIds[0])
	})
}

func TestGetUnits(t *testing.T) {
	observe := testobservability.Default(t)
	unitID1 := append(make(types.UnitID, 31), 1, 1) // id=1 type=1
	unitID2 := append(make(types.UnitID, 31), 2, 1) // id=2 type=1
	unitID3 := append(make(types.UnitID, 31), 3, 1) // id=3 type=1
	unitID4 := append(make(types.UnitID, 31), 4, 2) // id=4 type=2
	unitID5 := append(make(types.UnitID, 31), 5, 2) // id=5 type=2
	node := &MockNode{
		txs: &testtxsystem.CounterTxSystem{
			FixedState: prepareState(t, unitID1, unitID2, unitID3, unitID4, unitID5),
		},
	}
	pdr := &types.PartitionDescriptionRecord{
		Version:     1,
		NetworkID:   types.NetworkLocal,
		PartitionID: tokens.DefaultPartitionID,
		TypeIDLen:   8,
		UnitIDLen:   8 * 32,
		T2Timeout:   2500 * time.Millisecond,
	}
	api := NewStateAPI(node, observe, WithGetUnits(true), WithShardConf(pdr))

	t.Run("ok", func(t *testing.T) {
		unitIDs, err := api.GetUnits(nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, unitIDs, 5)
	})
	t.Run("api disabled", func(t *testing.T) {
		api := NewStateAPI(node, observe, WithGetUnits(false), WithShardConf(pdr))
		typeID := uint32(3)
		unitIDs, err := api.GetUnits(&typeID, nil, nil)
		require.ErrorContains(t, err, "state_getUnits is disabled")
		require.Nil(t, unitIDs)
	})
	t.Run("pagination", func(t *testing.T) {
		limit := 2
		unitIDs, err := api.GetUnits(nil, nil, &limit)
		require.NoError(t, err)
		require.Len(t, unitIDs, 2)
		require.EqualValues(t, unitID1, unitIDs[0])
		require.EqualValues(t, unitID2, unitIDs[1])

		unitIDs, err = api.GetUnits(nil, &unitIDs[1], &limit)
		require.NoError(t, err)
		require.Len(t, unitIDs, 2)
		require.EqualValues(t, unitID3, unitIDs[0])
		require.EqualValues(t, unitID4, unitIDs[1])

		unitIDs, err = api.GetUnits(nil, &unitIDs[1], &limit)
		require.NoError(t, err)
		require.Len(t, unitIDs, 1)
		require.EqualValues(t, unitID5, unitIDs[0])

		unitIDs, err = api.GetUnits(nil, &unitIDs[0], &limit)
		require.NoError(t, err)
		require.Len(t, unitIDs, 0)
	})
	t.Run("limit", func(t *testing.T) {
		api := NewStateAPI(node, observe, WithGetUnits(true), WithShardConf(pdr), WithResponseItemLimit(1))

		unitIDs, err := api.GetUnits(nil, nil, nil)
		require.NoError(t, err)
		require.Len(t, unitIDs, 1)
		require.EqualValues(t, unitID1, unitIDs[0])

		limit := 2
		unitIDs, err = api.GetUnits(nil, nil, &limit)
		require.NoError(t, err)
		require.Len(t, unitIDs, 1)
		require.EqualValues(t, unitID1, unitIDs[0])
	})
}

func TestSendTransaction(t *testing.T) {
	observe := testobservability.Default(t)

	t.Run("ok", func(t *testing.T) {
		node := &MockNode{}
		api := NewStateAPI(node, observe)
		tx := createTransactionOrder(t, []byte{1})
		txHash, err := api.SendTransaction(context.Background(), tx)
		require.NoError(t, err)
		require.NotNil(t, txHash)
	})

	t.Run("err", func(t *testing.T) {
		expErr := errors.New("failed to process tx")
		failingUnitID := test.RandomBytes(33)
		node := &MockNode{
			onSubmitTx: func(ctx context.Context, to *types.TransactionOrder) ([]byte, error) {
				if bytes.Equal(to.UnitID, failingUnitID) {
					return nil, expErr
				}
				return nil, nil
			},
		}
		api := NewStateAPI(node, observe)
		tx := createTransactionOrder(t, failingUnitID)
		txHash, err := api.SendTransaction(context.Background(), tx)
		require.ErrorIs(t, err, expErr)
		require.Nil(t, txHash)
	})
}

func TestGetTransactionProof(t *testing.T) {
	observe := testobservability.Default(t)
	node := &MockNode{}
	api := NewStateAPI(node, observe)

	t.Run("ok", func(t *testing.T) {
		txHash := []byte{1}
		res, err := api.GetTransactionProof(context.Background(), txHash)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.NotNil(t, res.TxRecordProof)

		var txRecordProof *types.TxRecordProof
		err = types.Cbor.Unmarshal(res.TxRecordProof, &txRecordProof)
		require.NoError(t, err)
		require.NotNil(t, txRecordProof)
	})
	t.Run("err", func(t *testing.T) {
		txHash := []byte{1}
		node.err = errors.New("some error")

		res, err := api.GetTransactionProof(context.Background(), txHash)
		require.ErrorContains(t, err, "some error")
		require.Nil(t, res)
	})
}

func TestGetBlock(t *testing.T) {
	observe := testobservability.Default(t)
	node := &MockNode{}
	api := NewStateAPI(node, observe)

	t.Run("ok", func(t *testing.T) {
		node.maxBlockNumber = 1
		blockNumber := hex.Uint64(1)
		res, err := api.GetBlock(context.Background(), blockNumber)
		require.NoError(t, err)
		require.NotNil(t, res)

		var block *types.Block
		err = types.Cbor.Unmarshal(res, &block)
		require.NoError(t, err)
		rn, err := block.GetRoundNumber()
		require.NoError(t, err)
		require.EqualValues(t, 1, rn)
	})
	t.Run("block not found", func(t *testing.T) {
		node.maxBlockNumber = 1
		blockNumber := hex.Uint64(2)

		res, err := api.GetBlock(context.Background(), blockNumber)
		require.NoError(t, err)
		require.Nil(t, res)
	})
}

func TestGetTrustBase(t *testing.T) {
	observe := testobservability.Default(t)
	node := &MockNode{}
	api := NewStateAPI(node, observe)

	t.Run("ok", func(t *testing.T) {
		_, verifier := testsig.CreateSignerAndVerifier(t)
		trustBase, err := types.NewTrustBaseGenesis(types.NetworkMainNet, []*types.NodeInfo{trustbase.NewNodeInfoFromVerifier(t, "1", verifier)})
		require.NoError(t, err)
		node.trustBase = trustBase

		res, err := api.GetTrustBase(1)
		require.NoError(t, err)
		require.NotNil(t, res)
		require.Equal(t, trustBase, res)

	})
	t.Run("err", func(t *testing.T) {
		node.err = errors.New("trust base not found")

		res, err := api.GetTrustBase(1)
		require.ErrorContains(t, err, "trust base not found")
		require.Nil(t, res)
	})
}

func prepareState(t *testing.T, unitIDs ...types.UnitID) *state.State {
	s := state.NewEmptyState()
	for _, unitID := range unitIDs {
		require.NoError(t, s.Apply(
			state.AddUnit(unitID, &unitData{I: 10, O: templates.AlwaysTrueBytes()}),
		))
		require.NoError(t, s.AddUnitLog(unitID, test.RandomBytes(32)))
	}

	summaryValue, summaryHash, err := s.CalculateRoot()
	require.NoError(t, err)
	require.NoError(t, s.Commit(&types.UnicityCertificate{Version: 1, InputRecord: &types.InputRecord{
		Version:      1,
		RoundNumber:  1,
		Hash:         summaryHash,
		SummaryValue: util.Uint64ToBytes(summaryValue),
	}}))
	return s
}

type unitData struct {
	_ struct{} `cbor:",toarray"`
	I uint64
	O []byte
}

func (ud *unitData) Hash(hashAlgo crypto.Hash) ([]byte, error) {
	hasher := abhash.New(hashAlgo.New())
	ud.Write(hasher)
	return hasher.Sum()
}

func (ud *unitData) Write(hasher abhash.Hasher) {
	hasher.Write(ud)
}

func (ud *unitData) SummaryValueInput() uint64 {
	return ud.I
}

func (ud *unitData) Copy() types.UnitData {
	return &unitData{I: ud.I, O: ud.O}
}

func (ud *unitData) Owner() []byte {
	return ud.O
}

func (ud *unitData) GetVersion() types.Version {
	return 0
}

type (
	MockNode struct {
		maxBlockNumber     uint64
		maxRoundNumber     uint64
		currentEpochNumber uint64
		transactions       []*types.TransactionOrder
		err                error
		txs                txsystem.TransactionSystem
		trustBase          types.RootTrustBase

		onSubmitTx func(context.Context, *types.TransactionOrder) ([]byte, error)
	}

	MockOwnerIndex struct {
		err        error
		ownerUnits map[string][]types.UnitID
	}
)

func (mn *MockNode) TransactionSystemState() txsystem.StateReader {
	return mn.txs.State()
}

func (mn *MockNode) GetTransactionRecordProof(_ context.Context, hash []byte) (*types.TxRecordProof, error) {
	if mn.err != nil {
		return nil, mn.err
	}
	return &types.TxRecordProof{}, nil
}

func (mn *MockNode) SubmitTx(ctx context.Context, tx *types.TransactionOrder) ([]byte, error) {
	if mn.onSubmitTx != nil {
		return mn.onSubmitTx(ctx, tx)
	}

	mn.transactions = append(mn.transactions, tx)
	return tx.Hash(crypto.SHA256)
}

func (mn *MockNode) GetBlock(_ context.Context, blockNumber uint64) (*types.Block, error) {
	if mn.err != nil {
		return nil, mn.err
	}
	if blockNumber > mn.maxBlockNumber {
		// empty block
		return nil, nil
	}
	uc, err := (&types.UnicityCertificate{Version: 1, InputRecord: &types.InputRecord{Version: 1, RoundNumber: blockNumber}}).MarshalCBOR()
	if err != nil {
		return nil, err
	}
	return &types.Block{UnicityCertificate: uc}, nil
}

func (mn *MockNode) LatestBlockNumber() (uint64, error) {
	return mn.maxBlockNumber, nil
}

func (mn *MockNode) CurrentRoundInfo(_ context.Context) (*partition.RoundInfo, error) {
	if mn.err != nil {
		return nil, mn.err
	}
	return &partition.RoundInfo{RoundNumber: mn.maxRoundNumber, EpochNumber: mn.currentEpochNumber}, nil
}

func (mn *MockNode) NetworkID() types.NetworkID {
	return 5
}

func (mn *MockNode) PartitionID() types.PartitionID {
	return 0x00010000
}

func (mn *MockNode) PartitionTypeID() types.PartitionTypeID {
	if mn.txs != nil {
		return mn.txs.TypeID()
	}
	return 1
}

func (mn *MockNode) ShardID() types.ShardID {
	return types.ShardID{}
}

func (mn *MockNode) Peer() *network.Peer {
	return nil
}

func (mn *MockNode) Validators() peer.IDSlice {
	return []peer.ID{}
}

func (mn *MockNode) SerializeState(writer io.Writer) error {
	return mn.TransactionSystemState().Serialize(writer, true, nil)
}

func (mn *MockNode) GetTrustBase(epochNumber uint64) (types.RootTrustBase, error) {
	if mn.err != nil {
		return nil, mn.err
	}
	return mn.trustBase, nil
}

func (mn *MockNode) IsPermissionedMode() bool {
	return false
}

func (mn *MockNode) IsFeelessMode() bool {
	return false
}

func (mn *MockNode) RegisterShardConf(shardConf *types.PartitionDescriptionRecord) error {
	return nil
}

func (mn *MockOwnerIndex) GetOwnerUnits(ownerID []byte, sinceUnitID *types.UnitID, limit int) ([]types.UnitID, error) {
	if mn.err != nil {
		return nil, mn.err
	}
	startIndex := startIndex(sinceUnitID, mn.ownerUnits[string(ownerID)])
	if startIndex >= len(mn.ownerUnits[string(ownerID)]) {
		return []types.UnitID{}, nil
	}
	endIndex := endIndex(startIndex, limit, mn.ownerUnits[string(ownerID)])
	return mn.ownerUnits[string(ownerID)][startIndex:endIndex], nil
}

func createTransactionOrder(t *testing.T, unitID types.UnitID) []byte {
	bt := &money.TransferAttributes{
		NewOwnerPredicate: templates.AlwaysTrueBytes(),
		TargetValue:       1,
		Counter:           0,
	}

	attBytes, err := types.Cbor.Marshal(bt)
	require.NoError(t, err)

	txo := &types.TransactionOrder{
		Version: 1,
		Payload: types.Payload{
			UnitID:         unitID,
			Type:           money.TransactionTypeTransfer,
			Attributes:     attBytes,
			ClientMetadata: &types.ClientMetadata{Timeout: 0},
		},
	}
	authProof := money.TransferAuthProof{OwnerProof: []byte{1}}
	require.NoError(t, txo.SetAuthProof(authProof))

	txoCBOR, err := types.Cbor.Marshal(txo)
	require.NoError(t, err)
	return txoCBOR
}
