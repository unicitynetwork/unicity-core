package rootchain

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/alphabill-org/alphabill-go-base/crypto"
	"github.com/alphabill-org/alphabill-go-base/types"
	"github.com/alphabill-org/alphabill/internal/testutils/observability"
	"github.com/alphabill-org/alphabill/network/protocol/certification"
	"github.com/alphabill-org/alphabill/rootchain/partitions"
)

var ir1 = &types.InputRecord{Version: 1, Hash: []byte{1}}
var ir2 = &types.InputRecord{Version: 1, Hash: []byte{2}}
var ir3 = &types.InputRecord{Version: 1, Hash: []byte{3}}
var sysID1 = types.PartitionID(1)
var sysID2 = types.PartitionID(2)

var req1 = &certification.BlockCertificationRequest{
	InputRecord: ir1,
}

// Test internals
func Test_requestStore_add(t *testing.T) {
	rs := newRequestStore()
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	res, proof, err := rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.ErrorContains(t, err, "request of the node in this round already stored")
	require.Equal(t, QuorumUnknown, res)
	require.Nil(t, proof)
	// try to store a different IR
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir2}, trustBase)
	require.ErrorContains(t, err, "request of the node in this round already stored")
	require.Equal(t, QuorumUnknown, res)
	require.Nil(t, proof)
	// node 2 request
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.Len(t, proof, 2)
	require.Equal(t, 2, len(rs.nodeRequest))
	require.Equal(t, 1, len(rs.requests))
	_, res = rs.isConsensusReceived(trustBase)
	require.Equal(t, QuorumAchieved, res)
	for _, certReq := range rs.requests {
		require.Len(t, certReq, 2)
	}
}

func Test_requestStore_QuorumNotPossibleWith2Nodes(t *testing.T) {
	rs := newRequestStore()
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	res, proof, err := rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "2", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumNotPossible, res)
	require.Len(t, proof, 2)
	proof, res = rs.isConsensusReceived(trustBase)
	require.Equal(t, QuorumNotPossible, res)
	require.NotNil(t, proof)
}

func TestRequestStore_isConsensusReceived_SingleNode(t *testing.T) {
	rs := newRequestStore()
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil})
	res, proof, err := rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 1)
	require.Equal(t, req1.InputRecord, proof[0].InputRecord)
}

func TestRequestStore_isConsensusReceived_TwoNodes(t *testing.T) {
	rs := newRequestStore()
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	res, proof, err := rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "2", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumNotPossible, res)
	require.NotNil(t, proof)
	rs.reset()
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 2)
	require.Equal(t, req1.InputRecord, proof[0].InputRecord)
}

func TestRequestStore_isConsensusReceived_FiveNodes(t *testing.T) {
	rs := newRequestStore()
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil, "3": nil, "4": nil, "5": nil})
	// 1.
	res, proof, err := rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 2.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 3.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "3", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 3)
	// 4. change one filed and make sure it is not part of consensus
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "4", InputRecord: &types.InputRecord{Version: 1, Hash: bytes.Clone(ir1.Hash), SumOfEarnedFees: 10}}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	// proof is still 3, do not include invalid requests in quorum proof (at the moment)
	require.Len(t, proof, 3)
	// 5.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "5", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	// proof is still 3, do not include invalid requests in quorum proof (at the moment)
	require.Len(t, proof, 4)
}

func TestRequestStore_isConsensusNotPossible_FiveNodes(t *testing.T) {
	rs := newRequestStore()
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil, "3": nil, "4": nil, "5": nil})
	// 1.
	res, proof, err := rs.add(&certification.BlockCertificationRequest{NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 2.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 3.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "3", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 4.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "4", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 50/50 split, but validator 5 sends a unique req
	// 5.
	res, proof, err = rs.add(&certification.BlockCertificationRequest{NodeID: "5", InputRecord: ir3}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumNotPossible, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 5)
}

func TestCertRequestStore_isConsensusReceived_TwoNodes(t *testing.T) {
	obs := observability.Default(t)
	cs, err := NewCertificationRequestBuffer(obs.Meter("test"))
	require.NoError(t, err)
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	// 1.
	res, proof, err := cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	// 2. different IR than node 1, must result in QuorumNotPossible
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "2", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumNotPossible, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 2)
	shard := types.ShardID{}
	require.Equal(t, QuorumNotPossible, cs.IsConsensusReceived(sysID1, shard, trustBase))

	// clear the shard's buffer, should accept requests again - going for quorum this time
	cs.Clear(context.Background(), sysID1, shard)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 2)
	require.Equal(t, req1.InputRecord, proof[0].InputRecord)
	require.Equal(t, req1.InputRecord, proof[1].InputRecord)
}

func TestCertRequestStore_isConsensusReceived_MultiplePartitionIds(t *testing.T) {
	obs := observability.Default(t)
	cs, err := NewCertificationRequestBuffer(obs.Meter("test"))
	require.NoError(t, err)
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	shard := types.ShardID{}
	res, proof, err := cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID2, NodeID: "1", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	require.Equal(t, QuorumInProgress, cs.IsConsensusReceived(sysID1, shard, trustBase))
	require.Equal(t, QuorumInProgress, cs.IsConsensusReceived(sysID2, shard, trustBase))
	// add more requests
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	require.Len(t, proof, 2)
	require.Equal(t, ir1, proof[0].InputRecord)
	require.Equal(t, ir1, proof[1].InputRecord)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID2, NodeID: "2", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	require.Equal(t, QuorumAchieved, cs.IsConsensusReceived(sysID1, shard, trustBase))
	require.Equal(t, QuorumAchieved, cs.IsConsensusReceived(sysID2, shard, trustBase))
	require.Len(t, proof, 2)
	require.Equal(t, ir2, proof[0].InputRecord)
	require.Equal(t, ir2, proof[1].InputRecord)
	// Reset partition 1
	cs.Clear(context.Background(), sysID1, shard)
	require.Empty(t, cs.get(sysID1, shard).requests)
	require.Empty(t, cs.get(sysID1, shard).nodeRequest)
	require.Equal(t, QuorumInProgress, cs.IsConsensusReceived(sysID1, shard, trustBase))
	// partition 2 state mustn't have been reset
	require.Equal(t, QuorumAchieved, cs.IsConsensusReceived(sysID2, shard, trustBase))
}

func TestCertRequestStore_clearOne(t *testing.T) {
	obs := observability.Default(t)
	cs, err := NewCertificationRequestBuffer(obs.Meter("test"))
	require.NoError(t, err)
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	shard := types.ShardID{}
	res, proof, err := cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "1", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID2, NodeID: "1", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumInProgress, res)
	require.Nil(t, proof)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID1, NodeID: "2", InputRecord: ir1}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	res, proof, err = cs.Add(context.Background(), &certification.BlockCertificationRequest{PartitionID: sysID2, NodeID: "2", InputRecord: ir2}, trustBase)
	require.NoError(t, err)
	require.Equal(t, QuorumAchieved, res)
	require.NotNil(t, proof)
	// clear sys id 1
	cs.Clear(context.Background(), sysID1, shard)
	require.Empty(t, cs.get(sysID1, shard).requests)
	require.Empty(t, cs.get(sysID1, shard).nodeRequest)
	require.Equal(t, QuorumAchieved, cs.IsConsensusReceived(sysID2, shard, trustBase))
	require.Equal(t, QuorumInProgress, cs.IsConsensusReceived(sysID1, shard, trustBase))
}

func TestCertRequestStore_EmptyStore(t *testing.T) {
	obs := observability.Default(t)
	cs, err := NewCertificationRequestBuffer(obs.Meter("test"))
	require.NoError(t, err)
	shard := types.ShardID{}
	trustBase := partitions.NewPartitionTrustBase(map[string]crypto.Verifier{"1": nil, "2": nil})
	require.Empty(t, cs.get(sysID1, shard).requests)
	require.Empty(t, cs.get(sysID1, shard).nodeRequest)
	// Reset resets both stores
	part2 := types.PartitionID(0x1010101)
	require.NotPanics(t, func() { cs.Clear(context.Background(), sysID1, shard) })
	require.NotPanics(t, func() { cs.Clear(context.Background(), part2, shard) })
	require.Equal(t, QuorumInProgress, cs.IsConsensusReceived(sysID1, shard, trustBase))
	require.Equal(t, QuorumInProgress, cs.IsConsensusReceived(part2, shard, trustBase))
}
