package storage

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/unicitynetwork/bft-core/network/protocol/abdrc"
	"github.com/unicitynetwork/bft-core/network/protocol/certification"
	rctypes "github.com/unicitynetwork/bft-core/rootchain/consensus/types"
	"github.com/unicitynetwork/bft-go-base/types"
)

type (
	BlockStore struct {
		hash          crypto.Hash // hash algorithm
		blockTree     *BlockTree
		storage       PersistentStore
		orchestration Orchestration
		lock          sync.RWMutex
		log           *slog.Logger
	}

	PersistentStore interface {
		LoadBlocks() ([]*ExecutedBlock, error)
		WriteBlock(block *ExecutedBlock, root bool) error

		WriteVote(vote any) error
		ReadLastVote() (msg any, err error)

		WriteTC(tc *rctypes.TimeoutCert) error
		ReadLastTC() (*rctypes.TimeoutCert, error)
	}

	Orchestration interface {
		NetworkID() types.NetworkID
		ShardConfig(partition types.PartitionID, shard types.ShardID, rootRound uint64) (*types.PartitionDescriptionRecord, error)
		ShardConfigs(rootRound uint64) (map[types.PartitionShardID]*types.PartitionDescriptionRecord, error)
	}
)

func New(hashAlgo crypto.Hash, db PersistentStore, orchestration Orchestration, log *slog.Logger) (block *BlockStore, err error) {
	if db == nil {
		return nil, errors.New("storage is nil")
	}

	blTree, err := NewBlockTree(db, orchestration)
	if err != nil {
		return nil, fmt.Errorf("initializing block tree: %w", err)
	}
	return &BlockStore{
		hash:          hashAlgo,
		blockTree:     blTree,
		storage:       db,
		orchestration: orchestration,
		log:           log,
	}, nil
}

func NewFromState(hash crypto.Hash, block *abdrc.CommittedBlock, db PersistentStore, orchestration Orchestration, log *slog.Logger) (*BlockStore, error) {
	if db == nil {
		return nil, errors.New("storage is nil")
	}

	rootNode, err := NewRootBlock(block, hash, orchestration)
	if err != nil {
		return nil, fmt.Errorf("failed to create new root node: %w", err)
	}

	blTree, err := NewBlockTreeWithRootBlock(rootNode, db)
	if err != nil {
		return nil, fmt.Errorf("creating block tree from recovery: %w", err)
	}
	return &BlockStore{
		hash:          hash,
		blockTree:     blTree,
		storage:       db,
		orchestration: orchestration,
		log:           log,
	}, nil
}

func (x *BlockStore) ProcessTc(tc *rctypes.TimeoutCert) (rErr error) {
	if tc == nil {
		return fmt.Errorf("error tc is nil")
	}
	// persist last known TC
	if err := x.storage.WriteTC(tc); err != nil {
		// store DB error and continue
		rErr = fmt.Errorf("TC write failed: %w", err)
	}
	// Remove proposal/block for TC round if it exists, since quorum voted for timeout.
	// It will never be committed, hence it can be removed immediately.
	// It is fine if the block is not found, it does not matter anyway
	if err := x.blockTree.RemoveLeaf(tc.GetRound()); err != nil {
		return errors.Join(rErr, fmt.Errorf("removing timeout block %v: %w", tc.GetRound(), err))
	}
	return rErr
}

/*
IsChangeInProgress - return input record if shard has a pending IR change in the pipeline
or nil if no change is currently in the pipeline.
*/
func (x *BlockStore) IsChangeInProgress(partition types.PartitionID, shard types.ShardID) *types.InputRecord {
	k := types.PartitionShardID{PartitionID: partition, ShardID: shard.Key()}
	// go through the block we have and make sure that there is no change in progress for this shard
	for _, b := range x.blockTree.GetAllUncommittedNodes() {
		if _, ok := b.ShardState.Changed[k]; ok {
			return b.ShardState.States[k].IR
		}
	}
	return nil
}

func (x *BlockStore) GetDB() PersistentStore {
	return x.storage
}

func (x *BlockStore) ProcessQc(qc *rctypes.QuorumCert) ([]*certification.CertificationResponse, error) {
	if qc == nil {
		return nil, fmt.Errorf("qc is nil")
	}
	// if we have processed it already then skip (in case we are the next leader we have already handled the QC)
	if x.GetHighQc().GetRound() >= qc.GetRound() {
		// stale qc
		return nil, nil
	}
	// add Qc to block tree
	if err := x.blockTree.InsertQc(qc); err != nil {
		return nil, fmt.Errorf("failed to insert QC into block tree: %w", err)
	}
	// If the QC does not serve as commit QC, then we are done.
	// Non-commit QC has LedgerCommitInfo.RootChainRoundNumber == 0. It used to be LedgerCommitInfo.Hash == nil,
	// but now this is a committable value for new shards that have not yet agreed on the genesis state.
	if qc.LedgerCommitInfo.RootChainRoundNumber == 0 || qc.GetRound() == rctypes.GenesisRootRound {
		// NB! exception, no commit for genesis round
		return nil, nil
	}
	// If the QC commits a state committed block becomes the new root
	ucs, err := x.blockTree.Commit(qc)
	if err != nil {
		return nil, fmt.Errorf("committing new root block: %w", err)
	}
	return ucs, nil
}

// Add adds new round state to pipeline and returns the new state root hash a.k.a. execStateID
func (x *BlockStore) Add(block *rctypes.BlockData, verifier IRChangeReqVerifier) ([]byte, error) {
	// verify that block for the round does not exist yet
	// if block already exists, then check that it is the same block by comparing block hash
	if b, err := x.blockTree.FindBlock(block.GetRound()); err == nil && b != nil {
		b1h, err := b.BlockData.Hash(crypto.SHA256)
		if err != nil {
			return nil, fmt.Errorf("add block failed: cannot compute existing block's hash: %w", err)
		}
		b2h, err := block.Hash(crypto.SHA256)
		if err != nil {
			return nil, fmt.Errorf("add block failed: cannot compute block's hash %w", err)
		}
		// ignore if it is the same block, recovery may have added it when state was duplicated
		if bytes.Equal(b1h, b2h) {
			return b.RootHash, nil
		}
		return nil, fmt.Errorf("add block failed: different block for round %v is already in store", block.Round)
	}
	// block was not present, check parent block (QC round) is stored (if not node needs to recover)
	parentBlock, err := x.blockTree.FindBlock(block.GetParentRound())
	if err != nil {
		return nil, fmt.Errorf("add block failed: parent round %v not found, recover", block.Qc.VoteInfo.RoundNumber)
	}
	// Extend state from parent block
	exeBlock, err := parentBlock.Extend(block, verifier, x.orchestration, x.hash, x.log)
	if err != nil {
		return nil, fmt.Errorf("error processing block round %v, %w", block.Round, err)
	}
	// append new block
	if err = x.blockTree.Add(exeBlock); err != nil {
		return nil, fmt.Errorf("adding block to the tree: %w", err)
	}
	return exeBlock.RootHash, nil
}

func (x *BlockStore) GetHighQc() *rctypes.QuorumCert {
	return x.blockTree.HighQc()
}

func (x *BlockStore) GetLastTC() (*rctypes.TimeoutCert, error) {
	return x.storage.ReadLastTC()
}

func (x *BlockStore) GetCertificate(id types.PartitionID, shard types.ShardID) (*certification.CertificationResponse, error) {
	x.lock.RLock()
	defer x.lock.RUnlock()

	committedBlock := x.blockTree.Root()
	if si, ok := committedBlock.ShardState.States[types.PartitionShardID{PartitionID: id, ShardID: shard.Key()}]; ok {
		return si.LastCR, nil
	}
	return nil, fmt.Errorf("no certificate found for shard %s - %s", id, shard)
}

func (x *BlockStore) GetCertificates() []*types.UnicityCertificate {
	x.lock.RLock()
	defer x.lock.RUnlock()

	committedBlock := x.blockTree.Root()
	ucs := make([]*types.UnicityCertificate, 0, len(committedBlock.ShardState.States))
	for _, v := range committedBlock.ShardState.States {
		if v.LastCR != nil {
			ucs = append(ucs, &v.LastCR.UC)
		}
	}
	return ucs
}

func (x *BlockStore) ShardInfo(partition types.PartitionID, shard types.ShardID) *ShardInfo {
	x.lock.RLock()
	defer x.lock.RUnlock()

	committedBlock := x.blockTree.Root()
	if si, ok := committedBlock.ShardState.States[types.PartitionShardID{PartitionID: partition, ShardID: shard.Key()}]; ok {
		return si
	}
	return nil
}

func (x *BlockStore) GetState() (*abdrc.StateMsg, error) {
	return x.blockTree.CurrentState()
}

/*
Block returns block for given round.
When store doesn't have block for the round it returns error.
*/
func (x *BlockStore) Block(round uint64) (*ExecutedBlock, error) {
	return x.blockTree.FindBlock(round)
}

// StoreLastVote stores last sent vote message by this node
func (x *BlockStore) StoreLastVote(vote any) error {
	return x.storage.WriteVote(vote)
}

// ReadLastVote returns last sent vote message by this node
func (x *BlockStore) ReadLastVote() (any, error) {
	return x.storage.ReadLastVote()
}

func NewGenesisBlock(networkID types.NetworkID, hashAlgo crypto.Hash) (*ExecutedBlock, error) {
	genesisBlock := &rctypes.BlockData{
		Version:   1,
		Author:    "genesis",
		Round:     rctypes.GenesisRootRound,
		Epoch:     rctypes.GenesisRootEpoch,
		Timestamp: types.GenesisTime,
		Payload:   &rctypes.Payload{},
		Qc:        nil, // no parent block -> no parent QC
	}

	// Info about the round that commits the genesis block.
	// GenesisRootRound "produced" the genesis block and also commits it.
	commitRoundInfo := &rctypes.RoundInfo{
		Version:           1,
		RoundNumber:       genesisBlock.Round,
		Epoch:             genesisBlock.Epoch,
		Timestamp:         genesisBlock.Timestamp,
		ParentRoundNumber: 0,   // no parent block
		CurrentRootHash:   nil, // no shards -> Unicity Tree root hash is nil
	}
	commitRoundInfoHash, err := commitRoundInfo.Hash(hashAlgo)
	if err != nil {
		return nil, fmt.Errorf("failed to calculate round info hash: %w", err)
	}

	// QC that commits the genesis block
	commitQc := &rctypes.QuorumCert{
		VoteInfo: commitRoundInfo,
		LedgerCommitInfo: &types.UnicitySeal{
			Version:   1,
			NetworkID: networkID,
			// Usually the round that gets committed is different from
			// the round that commits, but for genesis block they are the same.
			RootChainRoundNumber: commitRoundInfo.RoundNumber,
			Epoch:                commitRoundInfo.Epoch,
			Timestamp:            commitRoundInfo.Timestamp,
			Hash:                 commitRoundInfo.CurrentRootHash,
			PreviousHash:         commitRoundInfoHash,
			Signatures:           nil, // QuorumCert.Signatures field is used
		},
		Signatures: nil, // root validators agree on the first block by running the same software, no need to sign
	}

	return &ExecutedBlock{
		BlockData: genesisBlock,
		HashAlgo:  hashAlgo,

		// the same QC accepts the genesis block and commits it, usually commit comes later
		Qc:       commitQc,
		CommitQc: commitQc,
		RootHash: commitQc.LedgerCommitInfo.Hash,
	}, nil
}
