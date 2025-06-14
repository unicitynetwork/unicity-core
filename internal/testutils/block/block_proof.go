package testblock

import (
	"crypto"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	test "github.com/unicitynetwork/bft-core/internal/testutils"
	testcertificates "github.com/unicitynetwork/bft-core/internal/testutils/certificates"
	abcrypto "github.com/unicitynetwork/bft-go-base/crypto"
	"github.com/unicitynetwork/bft-go-base/txsystem/money"
	"github.com/unicitynetwork/bft-go-base/types"
)

const (
	DefaultRoundNumber = 1
)

type (
	Options struct {
		pdr *types.PartitionDescriptionRecord
	}

	Option func(*Options)
)

func DefaultOptions() *Options {
	return &Options{
		pdr: DefaultPDR(),
	}
}

func DefaultPDR() *types.PartitionDescriptionRecord {
	return &types.PartitionDescriptionRecord{
		NetworkID:   5,
		PartitionID: money.DefaultPartitionID,
		T2Timeout:   2500 * time.Millisecond,
	}
}

func WithPartitionID(partitionID types.PartitionID) Option {
	return func(g *Options) {
		g.pdr.PartitionID = partitionID
	}
}

func CreateTxRecordProof(t *testing.T, txRecord *types.TransactionRecord, signer abcrypto.Signer, opts ...Option) *types.TxRecordProof {
	options := DefaultOptions()
	for _, option := range opts {
		option(options)
	}
	ir := &types.InputRecord{
		Version:      1,
		PreviousHash: make([]byte, 32),
		Hash:         test.RandomBytes(32),
		RoundNumber:  DefaultRoundNumber,
		SummaryValue: make([]byte, 32),
		Timestamp:    types.NewTimestamp(),
		ETHash:       test.RandomBytes(32),
	}
	b := CreateBlock(t, []*types.TransactionRecord{txRecord}, ir, options.pdr, signer)
	p, err := types.NewTxRecordProof(b, 0, crypto.SHA256)
	require.NoError(t, err)
	return p
}

func CreateBlock(t *testing.T, txs []*types.TransactionRecord, ir *types.InputRecord, pdr *types.PartitionDescriptionRecord, signer abcrypto.Signer) *types.Block {
	uc, err := (&types.UnicityCertificate{
		Version:     1,
		InputRecord: ir,
	}).MarshalCBOR()
	require.NoError(t, err)
	b := &types.Block{
		Header: &types.Header{
			Version:           1,
			PartitionID:       1,
			ProposerID:        "test",
			PreviousBlockHash: make([]byte, 32),
		},
		Transactions:       txs,
		UnicityCertificate: uc,
	}
	// calculate block hash
	ir, err = b.CalculateBlockHash(crypto.SHA256)
	require.NoError(t, err)
	b.UnicityCertificate, err = testcertificates.CreateUnicityCertificate(
		t,
		signer,
		ir,
		pdr,
		1,
		make([]byte, 32),
		make([]byte, 32),
	).MarshalCBOR()
	require.NoError(t, err)
	return b
}
