package genesis

import (
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"sort"

	abcrypto "github.com/alphabill-org/alphabill-go-base/crypto"
	abhash "github.com/alphabill-org/alphabill-go-base/hash"
	"github.com/alphabill-org/alphabill-go-base/types"
	"github.com/alphabill-org/alphabill-go-base/types/hex"
	"github.com/alphabill-org/alphabill-go-base/util"
	"github.com/alphabill-org/alphabill/network/protocol/certification"
	"github.com/alphabill-org/alphabill/network/protocol/genesis"
	rctypes "github.com/alphabill-org/alphabill/rootchain/consensus/types"
)

var ErrSignerIsNil = errors.New("signer is nil")

type (
	rootGenesisConf struct {
		peerID             string
		signer             abcrypto.Signer
		totalValidators    uint32
		blockRateMs        uint32
		consensusTimeoutMs uint32
		hashAlgorithm      crypto.Hash
	}

	Option func(c *rootGenesisConf)

	UnicitySealFunc func(rootHash []byte) (*types.UnicitySeal, error)
)

func (c *rootGenesisConf) isValid() error {
	if c.peerID == "" {
		return genesis.ErrNodeIDIsEmpty
	}
	if c.signer == nil {
		return ErrSignerIsNil
	}
	if c.totalValidators < 1 {
		return genesis.ErrInvalidNumberOfRootValidators
	}
	if c.consensusTimeoutMs < genesis.MinConsensusTimeout {
		return fmt.Errorf("invalid consensus timeout, must be at least %v", genesis.MinConsensusTimeout)
	}
	if c.blockRateMs < genesis.MinBlockRateMs {
		return fmt.Errorf("invalid block rate, must be at least %v", genesis.MinBlockRateMs)
	}
	// Timeout must be bigger than round min block-rate+2s
	if c.blockRateMs+genesis.MinConsensusTimeout > c.consensusTimeoutMs {
		return fmt.Errorf("invalid timeout for block rate, must be at least %d", c.blockRateMs+genesis.MinConsensusTimeout)
	}
	return nil
}

func WithTotalNodes(rootValidators uint32) Option {
	return func(c *rootGenesisConf) {
		c.totalValidators = rootValidators
	}
}

func WithBlockRate(rate uint32) Option {
	return func(c *rootGenesisConf) {
		c.blockRateMs = rate
	}
}

func WithConsensusTimeout(timeoutMs uint32) Option {
	return func(c *rootGenesisConf) {
		c.consensusTimeoutMs = timeoutMs
	}
}

// WithHashAlgorithm set custom hash algorithm (unused for now, remove?)
func WithHashAlgorithm(hashAlgorithm crypto.Hash) Option {
	return func(c *rootGenesisConf) {
		c.hashAlgorithm = hashAlgorithm
	}
}

func createUnicityCertificates(certs map[types.PartitionID]*types.UnicityCertificate, utData []*types.UnicityTreeData, hash crypto.Hash, sealFn UnicitySealFunc) ([]byte, error) {
	// calculate unicity tree
	ut, err := types.NewUnicityTree(hash, utData)
	if err != nil {
		return nil, fmt.Errorf("unicity tree calculation failed: %w", err)
	}
	// create seal
	rootHash := ut.RootHash()
	seal, err := sealFn(rootHash)
	if err != nil {
		return nil, fmt.Errorf("unicity seal generation failed: %w", err)
	}

	for _, d := range utData {
		utCert, err := ut.Certificate(d.Partition)
		if err != nil {
			return nil, fmt.Errorf("get unicity tree certificate error: %w", err)
		}
		uc := certs[d.Partition]
		uc.UnicityTreeCertificate = utCert
		uc.UnicitySeal = seal
	}

	return rootHash, nil
}

func partitionsFromNodes(nodes []*genesis.PartitionNode) ([]*genesis.GenesisPartitionRecord, error) {
	var partitionNodesMap = make(map[types.PartitionID][]*genesis.PartitionNode)
	for _, n := range nodes {
		if err := n.IsValid(); err != nil {
			return nil, fmt.Errorf("partition node %s validation failed: %w", n.NodeID, err)
		}
		si := n.BlockCertificationRequest.PartitionID
		partitionNodesMap[si] = append(partitionNodesMap[si], n)
	}

	var partitions []*genesis.GenesisPartitionRecord
	for _, partitionNodes := range partitionNodesMap {
		pr, err := newPartitionRecord(partitionNodes)
		if err != nil {
			return nil, fmt.Errorf("partition record generation error: %w", err)
		}
		partitions = append(partitions, pr)
	}

	// sort partitions by partition id
	sort.Slice(partitions, func(i, j int) bool {
		return partitions[i].PartitionDescription.PartitionID < partitions[j].PartitionDescription.PartitionID
	})

	return partitions, nil
}

func NewRootGenesis(
	nodeID string,
	signer abcrypto.Signer,
	nodes []*genesis.PartitionNode,
	opts ...Option,
) (*genesis.RootGenesis, []*genesis.PartitionGenesis, error) {
	c := &rootGenesisConf{
		peerID:             nodeID,
		signer:             signer,
		totalValidators:    1,
		blockRateMs:        genesis.DefaultBlockRateMs,
		consensusTimeoutMs: genesis.DefaultConsensusTimeout,
		hashAlgorithm:      crypto.SHA256,
	}
	for _, option := range opts {
		option(c)
	}
	if err := c.isValid(); err != nil {
		return nil, nil, fmt.Errorf("consensus parameters validation failed: %w", err)
	}
	verifier, err := signer.Verifier()
	if err != nil {
		return nil, nil, fmt.Errorf("verifier error, %w", err)
	}

	partitions, err := partitionsFromNodes(nodes)
	if err != nil {
		return nil, nil, fmt.Errorf("genesis partition record generation failed: %w", err)
	}

	// make sure that there are no duplicate partition id's in provided partition records
	if err = genesis.CheckPartitionPartitionIDsUnique(partitions); err != nil {
		return nil, nil, fmt.Errorf("partition genesis records not unique: %w", err)
	}

	// iterate over all partitions and make sure that all requests are matching and every node is represented
	ucData := make([]*types.UnicityTreeData, len(partitions))
	// remember partition description records hashes and partition id for verification
	pdrHashes := make(map[types.PartitionID][]byte, len(partitions))
	certs := make(map[types.PartitionID]*types.UnicityCertificate)
	for i, partition := range partitions {
		partitionID := partition.PartitionDescription.PartitionID

		pdrHash, err := partition.PartitionDescription.Hash(c.hashAlgorithm)
		if err != nil {
			return nil, nil, fmt.Errorf("calculating partition %s description hash: %w", partitionID, err)
		}
		// if partition is valid then conversion cannot fail
		pdrHashes[partition.PartitionDescription.PartitionID] = pdrHash

		ir := partition.Validators[0].BlockCertificationRequest.InputRecord
		nodeIDs := util.TransformSlice(partition.Validators, func(pn *genesis.PartitionNode) string { return pn.NodeID })
		tr, err := TechnicalRecord(ir, nodeIDs)
		if err != nil {
			return nil, nil, fmt.Errorf("creating TR: %w", err)
		}
		trHash, err := tr.Hash()
		if err != nil {
			return nil, nil, fmt.Errorf("calculating partition %s TR hash: %w", partitionID, err)
		}

		// single shard partitions so we can create shard tree and cert
		sTree, err := types.CreateShardTree(partition.PartitionDescription.Shards, []types.ShardTreeInput{{IR: ir, TRHash: trHash}}, c.hashAlgorithm)
		if err != nil {
			return nil, nil, fmt.Errorf("creating shard tree: %w", err)
		}
		stCert, err := sTree.Certificate(types.ShardID{})
		if err != nil {
			return nil, nil, fmt.Errorf("creating shard tree certificate: %w", err)
		}
		certs[partitionID] = &types.UnicityCertificate{
			Version:              1,
			InputRecord:          ir,
			TRHash:               trHash,
			ShardTreeCertificate: stCert,
		}
		// if it is valid it must have at least one validator with a valid certification request
		// if there is more, all input records are matching
		ucData[i] = &types.UnicityTreeData{
			Partition:     partitionID,
			ShardTreeRoot: sTree.RootHash(),
			PDRHash:       pdrHash,
		}
	}
	// if all requests match then consensus is present
	sealFn := func(rootHash []byte) (*types.UnicitySeal, error) {
		roundMeta := &rctypes.RoundInfo{
			RoundNumber:       genesis.RootRound,
			Epoch:             0,
			Timestamp:         types.GenesisTime,
			ParentRoundNumber: 0,
			CurrentRootHash:   rootHash,
		}
		h, err := roundMeta.Hash(crypto.SHA256)
		if err != nil {
			return nil, fmt.Errorf("round info hash error: %w", err)
		}
		uSeal := &types.UnicitySeal{
			Version:              1,
			RootChainRoundNumber: genesis.RootRound,
			Timestamp:            types.GenesisTime,
			PreviousHash:         h,
			Hash:                 rootHash,
		}
		return uSeal, uSeal.Sign(c.peerID, c.signer)
	}
	// calculate unicity tree
	rootHash, err := createUnicityCertificates(certs, ucData, c.hashAlgorithm, sealFn)
	if err != nil {
		return nil, nil, fmt.Errorf("unicity certificate generation failed: %w", err)
	}
	// create "temporary" trust base with a single root validator to verify self signature
	sigKey, err := verifier.MarshalPublicKey()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal signing key: %w", err)
	}
	rootValidators := []*types.NodeInfo{
		&types.NodeInfo{NodeID: nodeID, SigKey: sigKey, Stake: 1},
	}
	tb, err := types.NewTrustBaseGenesis(
		rootValidators,
		rootHash,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trust base: %w", err)
	}
	for partitionID, uc := range certs {
		// ignore "not found" cases, we just put it there and if not, then verify will fail anyway
		pdrHash := pdrHashes[partitionID]
		if err = uc.Verify(tb, c.hashAlgorithm, partitionID, pdrHash); err != nil {
			return nil, nil, fmt.Errorf("generated unicity certificate verification failed: %w", err)
		}
	}

	for _, p := range partitions {
		cert, f := certs[p.PartitionDescription.PartitionID]
		if !f {
			return nil, nil, fmt.Errorf("missing UnicityCertificate for partition %s", p.PartitionDescription.PartitionID)
		}
		p.Certificate = cert
	}
	// Sign the consensus and append signature
	consensusParams := &genesis.ConsensusParams{
		Version:             1,
		TotalRootValidators: c.totalValidators,
		BlockRateMs:         c.blockRateMs,
		ConsensusTimeoutMs:  c.consensusTimeoutMs,
		HashAlgorithm:       uint32(c.hashAlgorithm),
		Signatures:          make(map[string]hex.Bytes),
	}
	if err = consensusParams.Sign(c.peerID, c.signer); err != nil {
		return nil, nil, fmt.Errorf("consensus parameter sign error: %w", err)
	}
	genesisRoot := &genesis.GenesisRootRecord{
		Version:        1,
		RootValidators: rootValidators,
		Consensus:      consensusParams,
	}
	rootGenesis := &genesis.RootGenesis{
		Version:    1,
		Root:       genesisRoot,
		Partitions: partitions,
	}
	if err = rootGenesis.IsValid(); err != nil {
		return nil, nil, fmt.Errorf("root genesis validation failed: %w", err)
	}
	partitionGenesis := partitionGenesisFromRoot(rootGenesis)
	return rootGenesis, partitionGenesis, nil
}

func TechnicalRecord(ir *types.InputRecord, nodes []string) (tr certification.TechnicalRecord, err error) {
	if len(nodes) == 0 {
		return tr, errors.New("node list is empty")
	}

	tr = certification.TechnicalRecord{
		Round:  ir.RoundNumber + 1,
		Epoch:  ir.Epoch,
		Leader: nodes[0],
		// precalculated hash of CBOR(certification.StatisticalRecord{})
		StatHash: []uint8{0x24, 0xee, 0x26, 0xf4, 0xaa, 0x45, 0x48, 0x5f, 0x53, 0xaa, 0xb4, 0x77, 0x57, 0xd0, 0xb9, 0x71, 0x99, 0xa3, 0xd9, 0x5f, 0x50, 0xcb, 0x97, 0x9c, 0x38, 0x3b, 0x7e, 0x50, 0x24, 0xf9, 0x21, 0xff},
	}

	fees := map[string]uint64{}
	for _, v := range nodes {
		fees[v] = 0
	}
	h := abhash.New(crypto.SHA256.New())
	h.WriteRaw(types.RawCBOR{0xA0}) // empty map
	h.Write(fees)
	if tr.FeeHash, err = h.Sum(); err != nil {
		return tr, fmt.Errorf("calculating fee hash: %w", err)
	}

	return tr, nil
}

func partitionGenesisFromRoot(rg *genesis.RootGenesis) []*genesis.PartitionGenesis {
	partitionGenesis := make([]*genesis.PartitionGenesis, len(rg.Partitions))
	for i, partition := range rg.Partitions {
		var partitionValidators = make([]*types.NodeInfo, len(partition.Validators))
		for j, v := range partition.Validators {
			partitionValidators[j] = &types.NodeInfo{
				NodeID: v.NodeID,
				SigKey: v.SigKey,
				Stake: 1,
			}
		}
		partitionGenesis[i] = &genesis.PartitionGenesis{
			PartitionDescription: partition.PartitionDescription,
			Certificate:          partition.Certificate,
			PartitionValidators:  partitionValidators,
			Params:               partition.Validators[0].Params,
		}
	}
	return partitionGenesis
}

func newPartitionRecord(nodes []*genesis.PartitionNode) (*genesis.GenesisPartitionRecord, error) {
	// validate nodes
	for _, n := range nodes {
		if err := n.IsValid(); err != nil {
			return nil, fmt.Errorf("partition node %s genesis validation failed %w", n.NodeID, err)
		}
	}
	// all nodes expected to have the same PDR so we just take the first
	pr := &genesis.GenesisPartitionRecord{
		Version:              1,
		PartitionDescription: &nodes[0].PartitionDescriptionRecord,
		Validators:           nodes,
	}

	// do basic validation of partition record, cannot verify since the certificate is not signed yet
	if err := pr.IsValid(); err != nil {
		return nil, fmt.Errorf("genesis partition record validation failed: %w", err)
	}
	return pr, nil
}

func MergeRootGenesisFiles(rootGenesis []*genesis.RootGenesis) (*genesis.RootGenesis, []*genesis.PartitionGenesis, error) {
	// Take the first and start appending to it from the rest
	rg, rest := rootGenesis[0], rootGenesis[1:]
	if err := rg.IsValid(); err != nil {
		return nil, nil, fmt.Errorf("invalid root genesis input: %w", err)
	}
	consensusBytes, err := rg.Root.Consensus.SigBytes()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to get consensus bytes: %w", err)
	}
	nodeIds := map[string]struct{}{}
	for _, v := range rg.Root.RootValidators {
		nodeIds[v.NodeID] = struct{}{}
	}
	// Check and append
	for _, appendGen := range rest {
		if err := appendGen.IsValid(); err != nil {
			return nil, nil, fmt.Errorf("invalid root genesis input: %w", err)
		}
		// Check consensus parameters are same by comparing serialized bytes
		// Should probably write a compare method instead of comparing serialized struct
		appendConsensusBytes, err := appendGen.Root.Consensus.SigBytes()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to get append consensus bytes: %w", err)
		}
		if !bytes.Equal(consensusBytes, appendConsensusBytes) {
			return nil, nil, errors.New("not compatible root genesis files, consensus is different")
		}
		// append consensus signatures
		for k, v := range appendGen.Root.Consensus.Signatures {
			// skip, already present
			if _, found := rg.Root.Consensus.Signatures[k]; found {
				continue
			}
			rg.Root.Consensus.Signatures[k] = v
		}
		// Take a naive approach for start: append first, validate later
		// append root info
		for _, v := range appendGen.Root.RootValidators {
			if _, found := nodeIds[v.NodeID]; found {
				continue
			}
			rg.Root.RootValidators = append(rg.Root.RootValidators, v)
			nodeIds[v.NodeID] = struct{}{}
		}
		// Make sure that they have same the number of partitions
		if len(rg.Partitions) != len(appendGen.Partitions) {
			return nil, nil, errors.New("not compatible root genesis files, different number of partitions")
		}
		// Append to UC Seal signatures, assume partitions are in the same order
		for i, rgPart := range rg.Partitions {
			rgPartSdh := rgPart.Certificate.UnicityTreeCertificate.PDRHash
			appendPart := appendGen.Partitions[i]
			if !bytes.Equal(rgPartSdh, appendPart.Certificate.UnicityTreeCertificate.PDRHash) {
				return nil, nil, errors.New("not compatible genesis files, partitions are different")
			}
			// copy partition UC Seal signatures
			for k, v := range appendPart.Certificate.UnicitySeal.Signatures {
				rgPart.Certificate.UnicitySeal.Signatures[k] = v
			}
		}
	}
	// verify result
	if err := rg.IsValid(); err != nil {
		return nil, nil, fmt.Errorf("root genesis combine failed: %w", err)
	}
	// extract new partition genesis files
	partitionGenesis := partitionGenesisFromRoot(rg)
	return rg, partitionGenesis, nil
}

func RootGenesisAddSignature(rootGenesis *genesis.RootGenesis, nodeID string, signer abcrypto.Signer) (*genesis.RootGenesis, error) {
	if rootGenesis == nil {
		return nil, fmt.Errorf("error, root genesis is nil")
	}
	if err := rootGenesis.IsValid(); err != nil {
		return nil, fmt.Errorf("invalid root genesis: %w", err)
	}
	if uint32(len(rootGenesis.Root.Consensus.Signatures)) >= rootGenesis.Root.Consensus.TotalRootValidators {
		return nil, fmt.Errorf("genesis is already signed by maximum number of root nodes")
	}
	// check already signed by node
	if _, found := rootGenesis.Root.Consensus.Signatures[nodeID]; found {
		return nil, fmt.Errorf("genesis is already signed by node id %v", nodeID)
	}
	if err := rootGenesis.Root.Consensus.Sign(nodeID, signer); err != nil {
		return nil, fmt.Errorf("add signature failed: %w", err)
	}
	sigVerifier, err := signer.Verifier()
	if err != nil {
		return nil, fmt.Errorf("get verifier failed: %w", err)
	}
	sigKey, err := sigVerifier.MarshalPublicKey()
	if err != nil {
		return nil, fmt.Errorf("marsha signing key failed: %w", err)
	}
	rootGenesis.Root.RootValidators = append(rootGenesis.Root.RootValidators,
		&types.NodeInfo{
			NodeID: nodeID,
			SigKey: sigKey,
			Stake:  1,
		})

	// Update partition records
	for _, pr := range rootGenesis.Partitions {
		if err = pr.Certificate.UnicitySeal.Sign(nodeID, signer); err != nil {
			return nil, fmt.Errorf("failed to sign partition %X seal: %w", pr.PartitionDescription.PartitionID, err)
		}
	}
	// make sure it what we signed is also valid
	if err = rootGenesis.IsValid(); err != nil {
		return nil, fmt.Errorf("root genesis validation failed: %w", err)
	}
	return rootGenesis, nil
}
