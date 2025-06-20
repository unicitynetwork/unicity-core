package money

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/unicitynetwork/bft-core/txsystem/fc/testutils"
	testtransaction "github.com/unicitynetwork/bft-core/txsystem/testutils/transaction"
	abcrypto "github.com/unicitynetwork/bft-go-base/crypto"
	moneyid "github.com/unicitynetwork/bft-go-base/testutils/money"
	"github.com/unicitynetwork/bft-go-base/types"
)

func TestTxRecording(t *testing.T) {
	const unknownPartitionID types.PartitionID = 0x01020304
	f := newFeeCreditTxRecorder(nil, 0, nil)
	signer, _ := abcrypto.NewInMemorySecp256K1Signer()
	pdr := moneyid.PDR()

	transferFCAmount := uint64(10)
	transferFCFee := uint64(1)
	attr := testutils.NewTransferFCAttr(t, signer, testutils.WithAmount(transferFCAmount))
	f.recordTransferFC(
		&transferFeeCreditTx{
			tx: testutils.NewTransferFC(t, signer,
				attr,
				testtransaction.WithPartitionID(moneyPartitionID),
			),
			fee:  transferFCFee,
			attr: attr,
		},
	)

	closeFCAmount := uint64(20)
	closeFCFee := uint64(2)
	reclaimFCFee := uint64(3)

	closeFCAttr := testutils.NewCloseFCAttr(testutils.WithCloseFCAmount(closeFCAmount))
	closureTx := testutils.WithReclaimFCClosureProof(&types.TxRecordProof{
		TxRecord: &types.TransactionRecord{
			Version:          1,
			TransactionOrder: testtransaction.TxoToBytes(t, testutils.NewCloseFC(t, signer, closeFCAttr)),
			ServerMetadata:   &types.ServerMetadata{ActualFee: closeFCFee},
		},
	})
	newReclaimFCAttr := testutils.NewReclaimFCAttr(t, &pdr, signer, closureTx)
	f.recordReclaimFC(
		&reclaimFeeCreditTx{
			tx:            testutils.NewReclaimFC(t, &pdr, signer, newReclaimFCAttr, testtransaction.WithPartitionID(moneyPartitionID)),
			attr:          newReclaimFCAttr,
			reclaimAmount: closeFCAttr.Amount - closeFCFee,
			reclaimFee:    reclaimFCFee,
			closeFee:      closeFCFee,
		},
	)

	addedCredit := f.getAddedCredit(moneyPartitionID)
	require.EqualValues(t, transferFCAmount-transferFCFee, addedCredit)
	require.EqualValues(t, 0, f.getAddedCredit(unknownPartitionID))

	reclaimedCredit := f.getReclaimedCredit(moneyPartitionID)
	require.EqualValues(t, closeFCAmount-closeFCFee, reclaimedCredit)
	require.EqualValues(t, 0, f.getReclaimedCredit(unknownPartitionID))

	spentFees := f.getSpentFeeSum()
	require.EqualValues(t, transferFCFee+reclaimFCFee, spentFees)
}
