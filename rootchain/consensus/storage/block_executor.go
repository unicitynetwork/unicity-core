package storage

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"log/slog"

	"github.com/alphabill-org/alphabill-go-base/hash"
	"github.com/alphabill-org/alphabill-go-base/types"
	"github.com/alphabill-org/alphabill-go-base/types/hex"
	"github.com/alphabill-org/alphabill/network/protocol/abdrc"
	"github.com/alphabill-org/alphabill/network/protocol/certification"
	rctypes "github.com/alphabill-org/alphabill/rootchain/consensus/types"
)

type (
	InputData struct {
		_             struct{} `cbor:",toarray"`
		Partition     types.PartitionID
		Shard         types.ShardID
		IR            *types.InputRecord
		Technical     certification.TechnicalRecord
		ShardConfHash hex.Bytes
	}

	InputRecords []*InputData
	ShardSet     map[types.PartitionShardID]struct{}

	ExecutedBlock struct {
		_         struct{}            `cbor:",toarray"`
		BlockData *rctypes.BlockData  // proposed block
		CurrentIR InputRecords        // all input records in this block
		Changed   ShardSet            // changed shard identifiers
		HashAlgo  crypto.Hash         // hash algorithm for the block
		RootHash  hex.Bytes           // resulting root hash
		Qc        *rctypes.QuorumCert // block's quorum certificate (from next view)
		CommitQc  *rctypes.QuorumCert // block's commit certificate
		ShardInfo shardStates
	}

	IRChangeReqVerifier interface {
		VerifyIRChangeReq(round uint64, irChReq *rctypes.IRChangeReq) (*InputData, error)
	}
)

func (data InputRecords) Update(newInputData *InputData) {
	for i, d := range data {
		if d.Partition == newInputData.Partition && d.Shard.Equal(newInputData.Shard) {
			data[i] = newInputData
			return
		}
	}
	data = append(data, newInputData)
}

func (data InputRecords) Find(partitionID types.PartitionID, shardID types.ShardID) *InputData {
	for _, d := range data {
		if d.Partition == partitionID && d.Shard.Equal(shardID) {
			return d
		}
	}
	return nil
}

/*
unicityTree builds the unicity tree based on the InputData slice.
*/
func (data InputRecords) UnicityTree(algo crypto.Hash) (*types.UnicityTree, map[types.PartitionShardID]*types.ShardTree, error) {
	// Construct UnicityTreeData for each partition (should group shards by partition first)
	// TODO: supports just single shard partitions, ie each element in the data slice is the sole shard of the partition!
	utData := make([]*types.UnicityTreeData, 0, len(data))
	shardTrees := make(map[types.PartitionShardID]*types.ShardTree)
	for _, d := range data {
		psID := types.PartitionShardID{PartitionID: d.Partition, ShardID: d.Shard.Key()}
		trHash, err := d.Technical.Hash()
		if err != nil {
			return nil, nil, fmt.Errorf("calculating TR hash: %w", err)
		}
		shardTree, err := types.CreateShardTree(
			types.ShardingScheme{},
			[]types.ShardTreeInput{
				{Shard: d.Shard, IR: d.IR, TRHash: trHash, ShardConfHash: d.ShardConfHash},
			}, algo)
		if err != nil {
			return nil, nil, fmt.Errorf("creating shard tree: %w", err)
		}
		shardTrees[psID] = &shardTree
		utData = append(utData, &types.UnicityTreeData{
			Partition:     d.Partition,
			ShardTreeRoot: shardTree.RootHash(),
		})
	}
	ut, err := types.NewUnicityTree(algo, utData)
	if err != nil {
		return nil, nil, err
	}
	return ut, shardTrees, nil
}

/*
certificationResponses builds the unicity tree and certification responses based on the InputData slice.
CertificationResponse will be generated only for shards listed in the "changed" argument. The UnicityCertificates
in the response are not complete, they miss the UnicityTreeCertificate and UnicitySeal.
*/
func (data InputRecords) certificationResponses(changed map[types.PartitionShardID]struct{}, algo crypto.Hash) ([]*certification.CertificationResponse, []byte, error) {
	ut, shardTrees, err := data.UnicityTree(algo)
	if err != nil {
		return nil, nil, fmt.Errorf("creating unicity tree: %w", err)
	}

	crs := []*certification.CertificationResponse{}
	// Generate an UC for each shard
	for _, d := range data {
		psID := types.PartitionShardID{PartitionID: d.Partition, ShardID: d.Shard.Key()}
		if _, ok := changed[psID]; ok {
			stCert, err := shardTrees[psID].Certificate(d.Shard)
			if err != nil {
				return nil, nil, fmt.Errorf("creating shard tree certificate: %w", err)
			}

			utCert, err := ut.Certificate(d.Partition)
			if err != nil {
				return nil, nil, fmt.Errorf("creating unicity tree certificate: %w", err)
			}

			trHash, err := d.Technical.Hash()
			if err != nil {
				return nil, nil, fmt.Errorf("calculating TR hash: %w", err)
			}

			crs = append(crs, &certification.CertificationResponse{
				Partition: d.Partition,
				Shard:     d.Shard,
				Technical: d.Technical,
				UC: types.UnicityCertificate{
					Version:                1,
					InputRecord:            d.IR,
					TRHash:                 trHash,
					ShardConfHash:          d.ShardConfHash,
					UnicityTreeCertificate: utCert,
					ShardTreeCertificate:   stCert,
				},
			})
		}
	}

	return crs, ut.RootHash(), err
}

func NewRootBlock(hash crypto.Hash, block *abdrc.CommittedBlock, orchestration Orchestration) (*ExecutedBlock, error) {
	changes := make(map[types.PartitionShardID]struct{})
	if block.Block.Payload != nil {
		// verify requests for IR change and proof of consensus
		for _, irChReq := range block.Block.Payload.Requests {
			changes[types.PartitionShardID{PartitionID: irChReq.Partition, ShardID: irChReq.Shard.Key()}] = struct{}{}
		}
	}

	irState := make(InputRecords, len(block.ShardInfo))
	shardInfo := shardStates{}
	for i, d := range block.ShardInfo {
		// EpochStart uniquely identifies the shardConf that is valid in this root round
		shardConf, err := orchestration.ShardConfig(d.Partition, d.Shard, d.EpochStart)
		if err != nil {
			return nil, fmt.Errorf("failed to load shard conf: %w", err)
		}
		shardConfHash, err := shardConf.Hash(crypto.SHA256)
		if err != nil {
			return nil, fmt.Errorf("calculating PDR hash: %w", err)
		}
		if !bytes.Equal(d.ShardConfHash, shardConfHash) {
			return nil, fmt.Errorf("calculated shard conf hash doesn't match the value in block data for %s - %s", d.Partition, d.Shard)
		}
		irState[i] = &InputData{
			Partition:     d.Partition,
			Shard:         d.Shard,
			IR:            d.IR,
			Technical:     d.IRTR,
			ShardConfHash: d.ShardConfHash,
		}

		si := &ShardInfo{
			PartitionID:   d.Partition,
			ShardID:       d.Shard,
			EpochStart:    d.EpochStart,
			T2Timeout:     d.T2Timeout,
			ShardConfHash: d.ShardConfHash,
			RootHash:      d.RootHash,
			PrevEpochStat: d.PrevEpochStat,
			Stat:          d.Stat,
			PrevEpochFees: d.PrevEpochFees,
			Fees:          d.Fees,
		}
		if d.UC != nil {
			si.LastCR = &certification.CertificationResponse{
				Partition: d.Partition,
				Shard:     d.Shard,
				Technical: *d.TR,
				UC:        *d.UC,
			}
		}
		if err := si.resetTrustBase(shardConf); err != nil {
			return nil, fmt.Errorf("initializing shard trustbase: %w", err)
		}
		shardInfo[types.PartitionShardID{PartitionID: d.Partition, ShardID: d.Shard.Key()}] = si
	}

	ut, _, err := irState.UnicityTree(hash)
	if err != nil {
		return nil, err
	}
	return &ExecutedBlock{
		BlockData: block.Block,
		CurrentIR: irState,
		Changed:   changes,
		HashAlgo:  hash,
		RootHash:  ut.RootHash(),
		Qc:        block.Qc,
		CommitQc:  block.CommitQc,
		ShardInfo: shardInfo,
	}, nil
}

func (x *ExecutedBlock) Extend(hash crypto.Hash, newBlock *rctypes.BlockData, verifier IRChangeReqVerifier, orchestration Orchestration, log *slog.Logger) (*ExecutedBlock, error) {
	// clone parent state
	irState := make(InputRecords, len(x.CurrentIR))
	copy(irState, x.CurrentIR)

	shardInfo, err := x.ShardInfo.nextBlock(irState, orchestration, newBlock.Round, hash)
	if err != nil {
		return nil, fmt.Errorf("creating shard info for the block: %w", err)
	}

	changes := make(map[types.PartitionShardID]struct{})

	// Create InputRecord for new shards
	for psID, si := range shardInfo {
		if irState.Find(si.PartitionID, si.ShardID) != nil {
			continue
		}
		ir, err := NewShardInputData(si, x.HashAlgo)
		if err != nil {
			return nil, fmt.Errorf("creating input record for new shard %s: %w", psID.String(), err)
		}
		irState = append(irState, ir)
		changes[psID] = struct{}{}
		log.Info(fmt.Sprintf("New shard activated: %s", psID.String()))
	}

	for _, irChReq := range newBlock.Payload.Requests {
		irData, err := verifier.VerifyIRChangeReq(newBlock.GetRound(), irChReq)
		if err != nil {
			return nil, fmt.Errorf("verifying change request: %w", err)
		}
		// timeout IR change request do not have BCR
		var req *certification.BlockCertificationRequest
		if len(irChReq.Requests) > 0 {
			req = irChReq.Requests[0]
		}
		si, ok := shardInfo[types.PartitionShardID{PartitionID: irChReq.Partition, ShardID: irChReq.Shard.Key()}]
		if !ok {
			return nil, fmt.Errorf("no shard info %s - %s", irChReq.Partition, irChReq.Shard)
		}

		prevIR := irState.Find(irChReq.Partition, irChReq.Shard)
		if irData.Technical, err = si.nextRound(req, prevIR.Technical, orchestration, newBlock.Round, hash); err != nil {
			return nil, fmt.Errorf("create TechnicalRecord: %w", err)
		}

		irState.Update(irData)
		changes[types.PartitionShardID{PartitionID: irChReq.Partition, ShardID: irChReq.Shard.Key()}] = struct{}{}
	}

	ut, _, err := irState.UnicityTree(hash)
	if err != nil {
		return nil, fmt.Errorf("creating UnicityTree: %w", err)
	}
	return &ExecutedBlock{
		BlockData: newBlock,
		CurrentIR: irState,
		Changed:   changes,
		HashAlgo:  hash,
		RootHash:  ut.RootHash(),
		ShardInfo: shardInfo,
	}, nil
}

func (x *ExecutedBlock) GenerateCertificates(commitQc *rctypes.QuorumCert) ([]*certification.CertificationResponse, error) {
	crs, rootHash, err := x.CurrentIR.certificationResponses(x.Changed, x.HashAlgo)
	if err != nil {
		return nil, fmt.Errorf("failed to generate unicity tree: %w", err)
	}
	// sanity check, data must not have changed, hence the root hash must still be the same
	if !bytes.Equal(rootHash, x.RootHash) {
		return nil, fmt.Errorf("root hash does not match previously calculated root hash")
	}
	// sanity check, if root hashes do not match then fall back to recovery
	if !bytes.Equal(rootHash, commitQc.LedgerCommitInfo.Hash) {
		return nil, fmt.Errorf("root hash does not match hash in commit QC")
	}
	// create UnicitySeal for pending certificates
	uSeal := &types.UnicitySeal{
		Version:              1,
		NetworkID:            commitQc.LedgerCommitInfo.NetworkID,
		RootChainRoundNumber: commitQc.LedgerCommitInfo.RootChainRoundNumber,
		Epoch:                commitQc.LedgerCommitInfo.Epoch,
		Hash:                 commitQc.LedgerCommitInfo.Hash,
		Timestamp:            commitQc.LedgerCommitInfo.Timestamp,
		PreviousHash:         commitQc.LedgerCommitInfo.PreviousHash,
		Signatures:           commitQc.Signatures,
	}
	ucs := []*certification.CertificationResponse{}
	for _, cr := range crs {
		cr.UC.UnicitySeal = uSeal
		ucs = append(ucs, cr)

		if si, ok := x.ShardInfo[types.PartitionShardID{PartitionID: cr.Partition, ShardID: cr.Shard.Key()}]; ok {
			si.LastCR = cr
		} else {
			return nil, fmt.Errorf("no SI for the shard %s - %s", cr.Partition, cr.Shard)
		}
	}
	return ucs, nil
}

func (x *ExecutedBlock) GetRound() uint64 {
	if x != nil {
		return x.BlockData.GetRound()
	}
	return 0
}

func (x *ExecutedBlock) GetParentRound() uint64 {
	if x != nil {
		return x.BlockData.GetParentRound()
	}
	return 0
}

/*
shardSetItem is helper type for serializing ShardSet - map with complex key
is not handled properly by the CBOR library so we serialize it as array.
*/
type shardSetItem struct {
	_         struct{} `cbor:",toarray"`
	Partition types.PartitionID
	Shard     []byte
}

func (ss ShardSet) MarshalCBOR() ([]byte, error) {
	// map with complex key is not handled properly by the CBOR library so we serialize it as array
	d := make([]shardSetItem, len(ss))
	idx := 0
	for k := range ss {
		d[idx].Partition = k.PartitionID
		d[idx].Shard = []byte(k.ShardID)
		idx++
	}
	buf := bytes.Buffer{}
	if err := types.Cbor.Encode(&buf, d); err != nil {
		return nil, fmt.Errorf("encoding shard set data: %w", err)
	}
	return buf.Bytes(), nil
}

func (ss *ShardSet) UnmarshalCBOR(data []byte) error {
	var d []shardSetItem
	if err := types.Cbor.Unmarshal(data, &d); err != nil {
		return fmt.Errorf("decoding shard set data: %w", err)
	}
	ssn := make(ShardSet, len(d))
	for _, itm := range d {
		ssn[types.PartitionShardID{PartitionID: itm.Partition, ShardID: string(itm.Shard)}] = struct{}{}
	}
	*ss = ssn
	return nil
}

func NewShardInputData(si *ShardInfo, hashAlgo crypto.Hash) (*InputData, error) {
	tr, err := newShardTechnicalRecord(si.nodeIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to create technical record for shard %d-%s: %w", si.PartitionID, si.ShardID, err)
	}

	return &InputData{
		Partition:     si.PartitionID,
		Shard:         si.ShardID,
		IR:            &types.InputRecord{Version: 1},
		ShardConfHash: si.ShardConfHash,
		Technical:     *tr,
	}, nil
}

func newShardTechnicalRecord(validators []string) (*certification.TechnicalRecord, error) {
	if len(validators) == 0 {
		return nil, errors.New("validator list empty")
	}

	tr := &certification.TechnicalRecord{
		Round:  1,
		Epoch:  0,
		Leader: validators[0],
		// precalculated hash of CBOR(certification.StatisticalRecord{})
		StatHash: []uint8{0x24, 0xee, 0x26, 0xf4, 0xaa, 0x45, 0x48, 0x5f, 0x53, 0xaa, 0xb4, 0x77, 0x57, 0xd0, 0xb9, 0x71, 0x99, 0xa3, 0xd9, 0x5f, 0x50, 0xcb, 0x97, 0x9c, 0x38, 0x3b, 0x7e, 0x50, 0x24, 0xf9, 0x21, 0xff},
	}

	fees := map[string]uint64{}
	for _, v := range validators {
		fees[v] = 0
	}
	h := hash.New(crypto.SHA256.New())
	h.WriteRaw(types.RawCBOR{0xA0}) // empty map
	h.Write(fees)

	var err error
	if tr.FeeHash, err = h.Sum(); err != nil {
		return tr, fmt.Errorf("calculating fee hash: %w", err)
	}

	return tr, nil
}
