package partition

import (
	"context"
	gocrypto "crypto"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	test "github.com/unicitynetwork/bft-core/internal/testutils"
	testevent "github.com/unicitynetwork/bft-core/internal/testutils/partition/event"
	"github.com/unicitynetwork/bft-core/internal/testutils/trustbase"
	testtxsystem "github.com/unicitynetwork/bft-core/internal/testutils/txsystem"
	"github.com/unicitynetwork/bft-core/keyvaluedb/memorydb"
	"github.com/unicitynetwork/bft-core/network"
	"github.com/unicitynetwork/bft-core/network/protocol/blockproposal"
	"github.com/unicitynetwork/bft-core/network/protocol/certification"
	"github.com/unicitynetwork/bft-core/partition/event"
	testtransaction "github.com/unicitynetwork/bft-core/txsystem/testutils/transaction"
	"github.com/unicitynetwork/bft-go-base/types"
	"github.com/unicitynetwork/bft-go-base/util"
)

type AlwaysValidCertificateValidator struct{}

func (c *AlwaysValidCertificateValidator) Validate(_ *types.UnicityCertificate, _ []byte) error {
	return nil
}

func TestNode_StartNewRoundCallsRInit(t *testing.T) {
	s := &testtxsystem.CounterTxSystem{}
	p := runSingleValidatorNodePartition(t, s)
	p.WaitHandshake(t)
	p.node.startNewRound(context.Background())
	// handshake sent us genesis UC which triggered new round and we then triggered it manually too
	require.Equal(t, uint64(2), s.BeginBlockCountDelta)
}

func TestNode_NodeStartTest(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	// node starts in init state
	require.Equal(t, initializing, tp.node.status.Load())
	// node sends a handshake to root
	test.TryTilCountIs(t, RequestReceived(tp, network.ProtocolHandshake), 4, test.WaitShortTick)
	// simulate no response, but monitor timeout
	tp.mockNet.ResetSentMessages(network.ProtocolHandshake)
	tp.SubmitMonitorTimeout(t)
	// node sends a handshake to root
	test.TryTilCountIs(t, RequestReceived(tp, network.ProtocolHandshake), 4, test.WaitShortTick)
	// while no response is received a retry is triggered on each timeout
	tp.mockNet.ResetSentMessages(network.ProtocolHandshake)
	tp.SubmitMonitorTimeout(t)
	// node sends a handshake
	test.TryTilCountIs(t, RequestReceived(tp, network.ProtocolHandshake), 4, test.WaitShortTick)
	tp.mockNet.ResetSentMessages(network.ProtocolHandshake)

	// root responds with initial uc
	tp.SubmitUnicityCertificate(t, tp.node.luc.Load())
	// node is initiated
	require.Eventually(t, func() bool {
		return tp.node.status.Load() == normal
	}, test.WaitDuration, test.WaitTick)
}

func TestNode_NodeStartWithRecoverStateFromDB(t *testing.T) {
	// used to generate test blocks
	txs := &testtxsystem.CounterTxSystem{FixedState: testtxsystem.MockState{}}
	tp := newSingleValidatorNodePartition(t, txs)

	// Replace blockStore before node is run
	db, err := memorydb.New()
	require.NoError(t, err)
	tp.nodeConf.blockStore = db

	// initial uc with stateHash == nil
	uc0 := txs.CommittedUC()
	// a copy so that we don't change the committed state in node
	txsCopy := txs.Clone()

	newBlock1, uc1 := createSameEpochBlock(t, tp, txsCopy, uc0) // the one that commits genesis state of txSystem
	newBlock2, uc2 := createSameEpochBlock(t, tp, txsCopy, uc1, testtransaction.NewTransactionRecord(t))
	newBlock3, uc3 := createNextEpochBlock(t, tp, txsCopy, uc2, testtransaction.NewTransactionRecord(t))
	newBlock4, uc4 := createSameEpochBlock(t, tp, txsCopy, uc3, testtransaction.NewTransactionRecord(t))
	newBlock5, _ := createSameEpochBlock(t, tp, txsCopy, uc4, testtransaction.NewTransactionRecord(t))
	require.NoError(t, db.Write(util.Uint64ToBytes(1), newBlock1))
	require.NoError(t, db.Write(util.Uint64ToBytes(2), newBlock2))
	require.NoError(t, db.Write(util.Uint64ToBytes(3), newBlock3))
	require.NoError(t, db.Write(util.Uint64ToBytes(4), newBlock4))
	// add transactions from block 5 as pending block
	require.NoError(t, db.Write(util.Uint32ToBytes(proposalKey), newBlock5))

	// start node with db filled
	ctx, cancel := context.WithCancel(context.Background())
	done := tp.start(ctx, t)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Fatal("partition node didn't shut down within timeout")
		}
	})

	// Ask Node for latest block
	b := tp.GetLatestBlock(t)
	rn, err := b.GetRoundNumber()
	require.NoError(t, err)
	require.Equal(t, uint64(4), rn)
	// Simulate UC received for block 5 - the pending block
	uc5, err := getUCv1(newBlock5)
	require.NoError(t, err)
	tp.SubmitUnicityCertificate(t, uc5)
	ContainsEventType(t, tp, event.BlockFinalized)
	b = tp.GetLatestBlock(t)
	rn, err = b.GetRoundNumber()
	require.NoError(t, err)
	require.Equal(t, uint64(5), rn)
}

func TestNode_CreateBlocks(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{FixedState: testtxsystem.MockState{}})
	tp.WaitHandshake(t)

	transfer := testtransaction.NewTransactionOrder(t)
	require.NoError(t, tp.SubmitTx(transfer))
	require.Eventually(t, func() bool {
		events := tp.eh.GetEvents()
		for _, e := range events {
			if e.EventType == event.TransactionProcessed {
				return true
			}
		}
		return false
	}, test.WaitDuration, test.WaitTick)
	tp.CreateBlock(t)

	block1 := tp.GetLatestBlock(t)
	require.NotEmpty(t, block1.GetProposerID())
	require.True(t, ContainsTransaction(t, block1, transfer))

	tx1 := testtransaction.NewTransactionOrder(t)
	require.NoError(t, tp.SubmitTxFromRPC(tx1))
	require.Eventually(t, func() bool {
		events := tp.eh.GetEvents()
		for _, e := range events {
			if e.EventType == event.TransactionProcessed {
				return true
			}
		}
		return false
	}, test.WaitDuration, test.WaitTick)
	tp.eh.Reset()
	tx2 := testtransaction.NewTransactionOrder(t)
	require.NoError(t, tp.SubmitTx(tx2))
	require.Eventually(t, func() bool {
		events := tp.eh.GetEvents()
		for _, e := range events {
			if e.EventType == event.TransactionProcessed {
				return true
			}
		}
		return false
	}, test.WaitDuration, test.WaitTick)
	tp.eh.Reset()
	tp.CreateBlock(t)

	block3 := tp.GetLatestBlock(t)
	require.True(t, ContainsTransaction(t, block3, tx1))
	require.True(t, ContainsTransaction(t, block3, tx2))
	require.False(t, ContainsTransaction(t, block3, transfer))

	_, err := tp.node.GetTransactionRecordProof(context.Background(), test.RandomBytes(33))
	require.ErrorIs(t, err, ErrIndexNotFound)
}

// create non-empty block #1 -> empty block #2 -> empty block #3 -> non-empty block #4
func TestNode_SubsequentEmptyBlocksNotPersisted(t *testing.T) {
	t.SkipNow()
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	genesis := tp.GetLatestBlock(t)
	tp.node.startNewRound(context.Background())
	require.NoError(t, tp.SubmitTx(testtransaction.NewTransactionOrder(t)))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)
	tp.CreateBlock(t)
	block1 := tp.GetLatestBlock(t)
	require.NotEmpty(t, block1.GetProposerID())
	uc, err := getUCv1(genesis)
	require.NoError(t, err)
	uc1, err := getUCv1(block1)
	require.NoError(t, err)
	require.NotEqual(t, uc.InputRecord.RoundNumber, uc1.InputRecord.RoundNumber)
	require.NotEqual(t, uc.InputRecord.BlockHash, uc1.InputRecord.BlockHash)

	// next block (empty)
	tp.CreateBlock(t)
	block2 := tp.GetLatestBlock(t) // this returns same block1 since empty block is not persisted
	require.Equal(t, block1, block2)
	// latest UC certifies empty block
	uc2 := tp.node.luc.Load()
	block2uc, err := getUCv1(block2)
	require.NoError(t, err)
	require.Less(t, block2uc.InputRecord.RoundNumber, uc2.InputRecord.RoundNumber)
	// hash of the latest certified empty block is zero-hash
	require.Nil(t, uc2.InputRecord.BlockHash)
	// state hash must stay the same as in last non-empty block
	require.Equal(t, block2uc.InputRecord.Hash, uc2.InputRecord.Hash)

	// next block (empty)
	tp.CreateBlock(t)
	require.Equal(t, block1, tp.GetLatestBlock(t))
	uc3 := tp.node.luc.Load()
	require.Less(t, uc2.InputRecord.RoundNumber, uc3.InputRecord.RoundNumber)
	require.Nil(t, uc3.InputRecord.BlockHash)
	block1uc, err := getUCv1(block1)
	require.NoError(t, err)
	require.Equal(t, block1uc.InputRecord.Hash, uc3.InputRecord.Hash)

	// next block (non-empty)
	require.NoError(t, tp.SubmitTx(testtransaction.NewTransactionOrder(t)))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)
	tp.CreateBlock(t)
	block4 := tp.GetLatestBlock(t)
	require.NotEmpty(t, block4.GetProposerID())
	require.NotEqual(t, block1, block4)
	block4uc, err := getUCv1(block4)
	require.NoError(t, err)
	require.Nil(t, block4uc.InputRecord.BlockHash)
	require.Equal(t, block1uc.InputRecord.BlockHash, block4.Header.PreviousBlockHash)
	uc4 := tp.node.luc.Load()
	require.Equal(t, block4uc, uc4)
	require.Equal(t, block1uc.InputRecord.Hash, uc4.InputRecord.PreviousHash)
	require.Less(t, uc3.InputRecord.RoundNumber, uc4.InputRecord.RoundNumber)
}

func TestNode_InvalidCertificateResponse(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	cr := &certification.CertificationResponse{
		Partition: tp.nodeConf.PartitionID(),
		Shard:     tp.nodeConf.ShardID(),
	}
	tp.mockNet.Receive(cr)
	ContainsError(t, tp, "invalid CertificationResponse: UnicityTreeCertificate is unassigned")
}

func TestNode_HandleStaleCertificationResponse(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)
	committedUC := tp.GetCommittedUC(t)
	transfer := testtransaction.NewTransactionOrder(t)

	require.NoError(t, tp.SubmitTx(transfer))
	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, committedUC), test.WaitDuration, test.WaitTick)

	tp.SubmitUnicityCertificate(t, committedUC)
	ContainsError(t, tp, "new certificate is from older root round 1 than previous certificate 2")
}

func TestNode_StartNodeBehindRootchain_OK(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	luc, found := tp.certs[tp.nodeConf.PartitionID()]
	require.True(t, found)
	// Mock and skip some root rounds
	tp.eh.Reset()
	tp.ReceiveCertResponseSameEpoch(t, luc.InputRecord, luc.UnicitySeal.RootChainRoundNumber+3)

	require.Eventually(t, func() bool {
		events := tp.eh.GetEvents()
		for _, e := range events {
			if e.EventType == event.NewRoundStarted {
				return true
			}
		}
		return false

	}, test.WaitDuration, test.WaitTick)
}

func TestNode_CreateEmptyBlock(t *testing.T) {
	txSystem := &testtxsystem.CounterTxSystem{}
	tp := runSingleValidatorNodePartition(t, txSystem)
	tp.WaitHandshake(t)
	tp.CreateBlock(t)

	uc1 := tp.GetCommittedUC(t) // genesis state
	txSystem.Revert()           // revert the state of the tx system
	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, uc1), test.WaitDuration, test.WaitTick)

	uc2 := tp.node.luc.Load()
	require.Equal(t, uc1.InputRecord.RoundNumber+1, uc2.InputRecord.RoundNumber)
	require.Equal(t, uc1.UnicityTreeCertificate.Partition, uc2.UnicityTreeCertificate.Partition)
	require.Equal(t, uc1.InputRecord.Hash, uc2.InputRecord.Hash)
	require.Equal(t, uc1.InputRecord.Hash, uc2.InputRecord.PreviousHash)
	require.Equal(t, uc1.InputRecord.SummaryValue, uc2.InputRecord.SummaryValue)

	// with no transactions, block hashes do not change
	require.Equal(t, uc1.InputRecord.BlockHash, uc2.InputRecord.BlockHash)
	require.Equal(t, uc1.UnicitySeal.RootChainRoundNumber+1, uc2.UnicitySeal.RootChainRoundNumber)
}

func TestNode_HandleEquivocatingUnicityCertificate_SameRoundDifferentIRHashes(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)
	uc1 := tp.GetCommittedUC(t)
	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, uc1), test.WaitDuration, test.WaitTick)
	block := tp.GetLatestBlock(t)
	require.NotNil(t, block)

	uc2 := tp.GetCommittedUC(t)
	ir := uc2.InputRecord.NewRepeatIR()
	ir.Hash = test.RandomBytes(32)
	ir.BlockHash = test.RandomBytes(32)

	tp.ReceiveCertResponseSameEpoch(t, ir, uc2.UnicitySeal.RootChainRoundNumber)
	ContainsError(t, tp, "equivocating UC, different input records for same partition round")
}

func TestNode_HandleEquivocatingUnicityCertificate_SameIRPreviousHashDifferentIRHash(t *testing.T) {
	txs := &testtxsystem.CounterTxSystem{FixedState: testtxsystem.MockState{}}
	tp := runSingleValidatorNodePartition(t, txs)
	tp.WaitHandshake(t)

	tp.node.startNewRound(context.Background())
	uc1 := tp.GetCommittedUC(t)
	txs.ExecuteCountDelta++ // so that the block is not considered empty
	require.NoError(t, tp.SubmitTx(testtransaction.NewTransactionOrder(t)))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)

	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, uc1), test.WaitDuration, test.WaitTick)

	uc2 := tp.GetCommittedUC(t)
	ir := uc2.InputRecord.NewRepeatIR()
	ir.Hash = test.RandomBytes(32)
	tp.ReceiveCertResponseSameEpoch(t, ir, uc2.UnicitySeal.RootChainRoundNumber+1)
	ContainsError(t, tp, "equivocating UC, different input records for same partition round")
}

// state does not change in case of no transactions
func TestNode_HandleUnicityCertificate_SameIR_DifferentBlockHash_StateReverted(t *testing.T) {
	txs := &testtxsystem.CounterTxSystem{FixedState: testtxsystem.MockState{}}
	tp := runSingleValidatorNodePartition(t, txs)
	tp.WaitHandshake(t)

	genesisUC := tp.node.luc.Load()
	tp.node.startNewRound(context.Background())
	require.NoError(t, tp.SubmitTx(testtransaction.NewTransactionOrder(t)))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)
	tp.CreateBlock(t)

	latestUC := tp.node.luc.Load()
	require.NotEqual(t, genesisUC, latestUC)
	tp.mockNet.ResetSentMessages(network.ProtocolBlockCertification)
	tp.node.startNewRound(context.Background())
	// create a new transaction
	require.NoError(t, tp.SubmitTx(testtransaction.NewTransactionOrder(t)))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)
	// create block proposal
	tp.SubmitT1Timeout(t)
	require.Equal(t, uint64(0), txs.RevertCount)

	// simulate receiving repeat UC
	tp.ReceiveCertResponseSameEpoch(t, latestUC.InputRecord.NewRepeatIR(), latestUC.UnicitySeal.RootChainRoundNumber+1)
	ContainsEventType(t, tp, event.StateReverted)
	require.Equal(t, uint64(1), txs.RevertCount)
}

func TestNode_HandleUnicityCertificate_ProposalIsNil(t *testing.T) {
	txSystem := &testtxsystem.CounterTxSystem{EndBlockChangesState: true}
	tp := runSingleValidatorNodePartition(t, txSystem)

	uc := tp.GetCommittedUC(t)
	txSystem.EndBlockCount = 10000

	ir := uc.InputRecord.NewRepeatIR()
	ir.RoundNumber++
	ir.SummaryValue = []byte{1}
	ir.Timestamp = 1
	ir.Hash = []byte{1, 2, 3}
	ir.BlockHash = []byte{3, 2, 1}
	tp.ReceiveCertResponseSameEpoch(t, ir, uc.UnicitySeal.RootChainRoundNumber+1)

	ContainsError(t, tp, ErrNodeDoesNotHaveLatestBlock.Error())
	require.Equal(t, uint64(1), txSystem.RevertCount)
	require.Equal(t, recovering, tp.node.status.Load())
}

// proposal not nil
// uc.InputRecord.Hash != n.pr.StateHash
// uc.InputRecord.Hash == n.pr.PrevHash
// => UC certifies the IR before pending block proposal ("repeat UC"). state is rolled back to previous state.
func TestNode_HandleUnicityCertificate_Revert(t *testing.T) {
	system := &testtxsystem.CounterTxSystem{EndBlockChangesState: true}
	tp := runSingleValidatorNodePartition(t, system)
	tp.WaitHandshake(t)
	uc := tp.GetCommittedUC(t)

	transfer := testtransaction.NewTransactionOrder(t)
	require.NoError(t, tp.SubmitTx(transfer))

	// create block proposal
	tp.SubmitT1Timeout(t)
	require.Equal(t, uint64(0), system.RevertCount)

	// send repeat UC
	ir := uc.InputRecord.NewRepeatIR()

	tp.ReceiveCertResponseSameEpoch(t, ir, uc.UnicitySeal.RootChainRoundNumber+1)
	ContainsEventType(t, tp, event.StateReverted)
	require.Equal(t, uint64(1), system.RevertCount)
}

// pending proposal exists
// uc.InputRecord.SumOfEarnedFees != n.pendingBlockProposal.SumOfEarnedFees
func TestNode_HandleUnicityCertificate_SumOfEarnedFeesMismatch_1(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{Fee: 1337})
	tp.WaitHandshake(t)

	// skip UC validation
	tp.node.conf.ucValidator = &AlwaysValidCertificateValidator{}

	// create the first block
	tp.CreateBlock(t)

	// send transaction that has a fee
	transferTx := testtransaction.NewTransactionOrder(t)
	require.NoError(t, tp.SubmitTx(transferTx))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)

	// when UC with modified IR.SumOfEarnedFees is received
	tp.SubmitT1Timeout(t)
	uc := tp.IssueBlockUC(t)
	uc.InputRecord = uc.InputRecord.NewRepeatIR()
	uc.InputRecord.SumOfEarnedFees += 1
	tp.SubmitUnicityCertificate(t, uc)

	// then state is reverted
	ContainsEventType(t, tp, event.StateReverted)
}

func TestNode_HandleUnicityCertificate_SwitchToNonValidator(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)

	require.Equal(t, 2, len(tp.node.Validators()))
	require.True(t, tp.node.IsValidator())

	// Create ShardConf for epoch 1
	// epoch 1 validators are [epoch0Node, thisNode, epoch1Node]
	shardConf1 := createShardConfWithNewNode(t, tp.nodeConf.shardConf)
	require.NoError(t, tp.node.RegisterShardConf(shardConf1))

	ir2 := tp.GetCommittedUC(t).InputRecord.NewRepeatIR()
	tp.ReceiveCertResponseWithEpoch(t, ir2, 200, 1)
	require.Eventually(t, func() bool {
		return len(tp.node.Validators()) == 3
	}, test.WaitDuration, test.WaitTick)
	require.True(t, tp.node.IsValidator())

	// Create ShardConf for epoch 2
	// epoch 2 validators are [epoch0Node, epoch1Node]
	shardConf2 := createShardConfWithRemovedNode(t, shardConf1, 1)
	require.NoError(t, tp.node.RegisterShardConf(shardConf2))

	tp.ReceiveCertResponseWithEpoch(t, ir2.NewRepeatIR(), 300, 2)
	require.Eventually(t, func() bool {
		return len(tp.node.Validators()) == 2
	}, test.WaitDuration, test.WaitTick)
	// This node has become a non-validator
	require.False(t, tp.node.IsValidator())

	// Create ShardConf for epoch 3
	// epoch 3 validators are [epoch0Node, thisNode, epoch1Node]
	shardConf3 := *shardConf1
	shardConf3.Epoch = 3
	shardConf3.EpochStart = 300
	require.Equal(t, 3, len(shardConf3.Validators))
	require.NoError(t, tp.node.RegisterShardConf(&shardConf3))

	tp.ReceiveCertResponseWithEpoch(t, ir2.NewRepeatIR(), 400, 3)
	require.Eventually(t, func() bool {
		return len(tp.node.Validators()) == 3
	}, test.WaitDuration, test.WaitTick)
	// This node has become a validator again
	require.True(t, tp.node.IsValidator())

	// Node is able to produce a block after becoming a validator again
	tp.CreateBlock(t)
}

func TestBlockProposal_BlockProposalIsNil(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.SubmitBlockProposal(nil)
	ContainsError(t, tp, blockproposal.ErrBlockProposalIsNil.Error())
}

func TestBlockProposal_InvalidNodeID(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)
	uc := tp.GetCommittedUC(t)
	transfer := testtransaction.NewTransactionOrder(t)

	require.NoError(t, tp.SubmitTx(transfer))
	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, uc), test.WaitDuration, test.WaitTick)
	tp.SubmitBlockProposal(&blockproposal.BlockProposal{NodeID: "1", UnicityCertificate: uc})
	ContainsError(t, tp, "block proposal from unknown node")
}

func TestBlockProposal_InvalidBlockProposal(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)
	uc := tp.GetCommittedUC(t)
	transfer := testtransaction.NewTransactionOrder(t)

	require.NoError(t, tp.SubmitTx(transfer))
	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, uc), test.WaitDuration, test.WaitTick)
	verifier, err := tp.rootSigner.Verifier()
	require.NoError(t, err)
	rootTrust := trustbase.NewTrustBase(t, verifier)
	val, err := NewDefaultBlockProposalValidator(
		tp.nodeConf.PartitionID(), tp.nodeConf.ShardID(), rootTrust, gocrypto.SHA256)
	require.NoError(t, err)
	tp.node.conf.bpValidator = val

	tp.SubmitBlockProposal(&blockproposal.BlockProposal{
		NodeID:             tp.nodeID(t),
		UnicityCertificate: uc,
	})

	ContainsError(t, tp, "invalid partition identifier")
}

func TestBlockProposal_HandleOldBlockProposal(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)
	uc := tp.GetCommittedUC(t)
	transfer := testtransaction.NewTransactionOrder(t)

	require.NoError(t, tp.SubmitTx(transfer))
	tp.CreateBlock(t)
	require.Eventually(t, NextBlockReceived(t, tp, uc), test.WaitDuration, test.WaitTick)

	tp.SubmitBlockProposal(&blockproposal.BlockProposal{
		NodeID:             tp.nodeID(t),
		PartitionID:        tp.nodeConf.PartitionID(),
		ShardID:            tp.nodeConf.ShardID(),
		UnicityCertificate: uc,
	})

	ContainsError(t, tp, "stale block proposal with UC from root round 1, LUC root round 2")
}

func TestBlockProposal_ExpectedLeaderInvalid(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	uc1 := tp.GetCommittedUC(t)
	uc2, tr, err := tp.CreateUnicityCertificate(t,
		uc1.InputRecord,
		uc1.UnicitySeal.RootChainRoundNumber+1,
	)
	require.NoError(t, err)

	// Submit UC2 so that LUC won't be updated from BlockProposal
	tp.SubmitUnicityCertificate(t, uc2)
	require.Eventually(t, func() bool {
		return tp.node.currentRoundNumber() == uc2.GetRoundNumber()+1
	}, test.WaitDuration, test.WaitTick)

	bp := &blockproposal.BlockProposal{
		PartitionID:        uc2.UnicityTreeCertificate.Partition,
		NodeID:             tp.nodeID(t),
		UnicityCertificate: uc2,
		Transactions:       []*types.TransactionRecord{},
		Technical:          *tr,
	}
	err = bp.Sign(gocrypto.SHA256, tp.nodeConf.signer)
	require.NoError(t, err)

	tp.node.leader.Set("foo")
	tp.SubmitBlockProposal(bp)
	ContainsError(t, tp, "expecting leader bQbp, leader in proposal:")
}

func TestBlockProposal_Ok(t *testing.T) {
	tp := runSingleValidatorNodePartition(t, &testtxsystem.CounterTxSystem{})
	tp.WaitHandshake(t)
	uc1 := tp.GetCommittedUC(t)
	uc2, _, err := tp.CreateUnicityCertificate(t,
		uc1.InputRecord,
		uc1.UnicitySeal.RootChainRoundNumber,
	)
	require.NoError(t, err)

	bp := &blockproposal.BlockProposal{
		PartitionID:        uc2.UnicityTreeCertificate.Partition,
		NodeID:             tp.nodeID(t),
		UnicityCertificate: uc2,
		Transactions:       []*types.TransactionRecord{},
	}
	err = bp.Sign(gocrypto.SHA256, tp.nodeConf.signer)
	require.NoError(t, err)
	tp.SubmitBlockProposal(bp)
	require.Eventually(t, RequestReceived(tp, network.ProtocolBlockCertification), test.WaitDuration, test.WaitTick)
}

func TestBlockProposal_TxSystemStateIsDifferent_sameUC(t *testing.T) {
	system := &testtxsystem.CounterTxSystem{}
	tp := runSingleValidatorNodePartition(t, system)
	tp.WaitHandshake(t)
	tp.CreateBlock(t)

	uc1 := tp.GetCommittedUC(t)
	uc2, _, err := tp.CreateUnicityCertificate(t,
		uc1.InputRecord,
		uc1.UnicitySeal.RootChainRoundNumber,
	)
	require.NoError(t, err)

	bp := &blockproposal.BlockProposal{
		PartitionID:        uc2.UnicityTreeCertificate.Partition,
		NodeID:             tp.nodeID(t),
		UnicityCertificate: uc2,
		Transactions:       []*types.TransactionRecord{},
	}
	err = bp.Sign(gocrypto.SHA256, tp.nodeConf.signer)
	require.NoError(t, err)
	system.InitCount = 10000
	tp.SubmitBlockProposal(bp)
	ContainsError(t, tp, "transaction system start state mismatch error, expected")
}

func TestBlockProposal_TxSystemStateIsDifferent_newUC(t *testing.T) {
	system := &testtxsystem.CounterTxSystem{}
	tp := runSingleValidatorNodePartition(t, system)
	tp.WaitHandshake(t)
	tp.CreateBlock(t)

	uc1 := tp.GetCommittedUC(t)
	// node receives a new UC with a block propsal that hints of a missing block
	ir := uc1.InputRecord.NewRepeatIR()
	ir.RoundNumber++
	ir.PreviousHash = ir.Hash
	ir.Hash = []byte{1, 2, 3}
	ir.BlockHash = []byte{3, 2, 1}
	uc2, tr, err := tp.CreateUnicityCertificate(t,
		ir,
		uc1.UnicitySeal.RootChainRoundNumber+1,
	)
	require.NoError(t, err)

	bp := &blockproposal.BlockProposal{
		PartitionID:        uc2.UnicityTreeCertificate.Partition,
		NodeID:             tp.nodeID(t),
		UnicityCertificate: uc2,
		Transactions:       []*types.TransactionRecord{},
		Technical:          *tr,
	}
	err = bp.Sign(gocrypto.SHA256, tp.nodeConf.signer)
	require.NoError(t, err)
	tp.SubmitBlockProposal(bp)
	ContainsError(t, tp, ErrNodeDoesNotHaveLatestBlock.Error())
	require.Equal(t, uint64(1), system.RevertCount)
	testevent.ContainsEvent(t, tp.eh, event.StateReverted)
	require.Equal(t, recovering, tp.node.status.Load())
}

func TestNode_GetTransactionRecord_OK(t *testing.T) {
	system := &testtxsystem.CounterTxSystem{FixedState: testtxsystem.MockState{}}
	indexDB, err := memorydb.New()
	require.NoError(t, err)
	tp := runSingleValidatorNodePartition(t, system, WithProofIndex(indexDB, 0))
	tp.WaitHandshake(t)
	require.NoError(t, tp.node.startNewRound(context.Background()))
	txo := testtransaction.NewTransactionOrder(t, testtransaction.WithTransactionType(21))
	hash, err := txo.Hash(tp.node.conf.hashAlgorithm)
	require.NoError(t, err)
	require.NoError(t, tp.SubmitTx(txo))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)

	txo2 := testtransaction.NewTransactionOrder(t, testtransaction.WithTransactionType(22))
	hash2, err := txo2.Hash(tp.node.conf.hashAlgorithm)
	require.NoError(t, err)
	require.NoError(t, tp.SubmitTxFromRPC(txo2))
	testevent.ContainsEvent(t, tp.eh, event.TransactionProcessed)
	tp.CreateBlock(t)

	require.Eventually(t, func() bool {
		proof, err := tp.node.GetTransactionRecordProof(context.Background(), hash)
		require.NoError(t, err)
		return proof != nil
	}, test.WaitDuration, test.WaitTick)

	require.Eventually(t, func() bool {
		proof, err := tp.node.GetTransactionRecordProof(context.Background(), hash2)
		require.NoError(t, err)
		return proof != nil
	}, test.WaitDuration, test.WaitTick)
}

func TestNode_ProcessInvalidTxInFeelessMode(t *testing.T) {
	txSystem := &testtxsystem.CounterTxSystem{
		FeelessMode:  true,
		ExecuteError: errors.New("failed to execute tx"),
	}

	indexDB, err := memorydb.New()
	require.NoError(t, err)
	tp := runSingleValidatorNodePartition(t, txSystem, WithProofIndex(indexDB, 0))
	tp.WaitHandshake(t)

	require.NoError(t, tp.node.startNewRound(context.Background()))

	txo := testtransaction.NewTransactionOrder(t, testtransaction.WithTransactionType(99))
	require.NoError(t, tp.SubmitTx(txo))
	testevent.ContainsEvent(t, tp.eh, event.TransactionFailed)

	currentRoundInfo, err := tp.node.CurrentRoundInfo(context.Background())
	require.NoError(t, err)
	tp.CreateBlock(t)

	// Failed transaction not put to block in feeless mode
	block, err := tp.node.GetBlock(context.Background(), currentRoundInfo.RoundNumber)
	require.NoError(t, err)
	require.Equal(t, 0, len(block.Transactions))
}

func TestNode_GetTransactionRecord_NotFound(t *testing.T) {
	system := &testtxsystem.CounterTxSystem{}
	db, err := memorydb.New()
	require.NoError(t, err)
	tp := runSingleValidatorNodePartition(t, system, WithProofIndex(db, 0))
	proof, err := tp.node.GetTransactionRecordProof(context.Background(), test.RandomBytes(32))
	require.ErrorIs(t, err, ErrIndexNotFound)
	require.Nil(t, proof)
}
