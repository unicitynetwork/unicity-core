package genesis

import (
	"testing"

	"github.com/alphabill-org/alphabill-go-base/types"
	testsig "github.com/alphabill-org/alphabill/internal/testutils/sig"
	"github.com/alphabill-org/alphabill/network/protocol/certification"
	"github.com/stretchr/testify/require"
)

const nodeID = "1"

func TestPartitionNode_IsValid_InvalidInputs(t *testing.T) {
	_, verifier := testsig.CreateSignerAndVerifier(t)
	pubKey, err := verifier.MarshalPublicKey()
	require.NoError(t, err)
	type fields struct {
		NodeID                           string
		SigKey                           []byte
		BlockCertificationRequestRequest *certification.BlockCertificationRequest
	}

	tests := []struct {
		name       string
		fields     fields
		wantErrStr string
	}{
		{
			name: "node identifier is empty",
			fields: fields{
				NodeID: "",
			},
			wantErrStr: ErrNodeIDIsEmpty.Error(),
		},
		{
			name: "signing public key is missing",
			fields: fields{
				NodeID: nodeID,
				SigKey: nil,
			},
			wantErrStr: ErrSigKeyIsInvalid.Error(),
		},
		{
			name: "signing public key is invalid",
			fields: fields{
				NodeID: "1",
				SigKey: []byte{0, 0, 0, 0},
			},
			wantErrStr: "invalid signing public key, pubkey must be 33 bytes long, but is 4",
		},
		{
			name: "invalid p1 request",
			fields: fields{
				NodeID:                           nodeID,
				SigKey:                           pubKey,
				BlockCertificationRequestRequest: nil,
			},
			wantErrStr: "block certification request validation failed, block certification request is nil",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			x := &PartitionNode{
				Version:                   1,
				NodeID:                    tt.fields.NodeID,
				SigKey:                    tt.fields.SigKey,
				BlockCertificationRequest: tt.fields.BlockCertificationRequestRequest,
			}
			err = x.IsValid()
			if tt.wantErrStr != "" {
				require.ErrorContains(t, err, tt.wantErrStr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestPartitionNodeIsValid(t *testing.T) {
	signer, verifier := testsig.CreateSignerAndVerifier(t)
	sigKey, err := verifier.MarshalPublicKey()
	require.NoError(t, err)
	req := &certification.BlockCertificationRequest{
		PartitionID: 1,
		NodeID:      nodeID,
		InputRecord: &types.InputRecord{
			Version:      1,
			PreviousHash: make([]byte, 32),
			Hash:         make([]byte, 32),
			BlockHash:    nil,
			SummaryValue: make([]byte, 32),
			Timestamp:    types.NewTimestamp(),
			RoundNumber:  1,
		},
	}
	require.NoError(t, req.Sign(signer))
	pn := &PartitionNode{
		Version:                   1,
		NodeID:                    nodeID,
		SigKey:                    sigKey,
		BlockCertificationRequest: req,
	}
	require.NoError(t, pn.IsValid())
}

func TestPartitionNode_IsValid_PartitionNodeIsNil(t *testing.T) {
	var pn *PartitionNode
	require.ErrorIs(t, ErrPartitionNodeIsNil, pn.IsValid())
}
