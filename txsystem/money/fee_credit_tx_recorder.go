package money

import (
	"fmt"

	"github.com/unicitynetwork/bft-go-base/txsystem/fc"
	"github.com/unicitynetwork/bft-go-base/txsystem/money"
	"github.com/unicitynetwork/bft-go-base/types"

	"github.com/unicitynetwork/bft-core/state"
)

// feeCreditTxRecorder container struct for recording fee credit transactions
type feeCreditTxRecorder struct {
	pdrs  map[types.PartitionID]*types.PartitionDescriptionRecord
	state *state.State
	// recorded fee credit transfers indexed by partition_identifier
	transferFeeCredits map[types.PartitionID][]*transferFeeCreditTx
	// recorded reclaim fee credit transfers indexed by partition_identifier
	reclaimFeeCredits map[types.PartitionID][]*reclaimFeeCreditTx
	partitionID       types.PartitionID
}

type transferFeeCreditTx struct {
	tx   *types.TransactionOrder
	fee  uint64
	attr *fc.TransferFeeCreditAttributes
}

type reclaimFeeCreditTx struct {
	tx            *types.TransactionOrder
	attr          *fc.ReclaimFeeCreditAttributes
	reclaimAmount uint64
	reclaimFee    uint64
	closeFee      uint64
}

func newFeeCreditTxRecorder(s *state.State, partitionID types.PartitionID, records []*types.PartitionDescriptionRecord) *feeCreditTxRecorder {
	sdrs := make(map[types.PartitionID]*types.PartitionDescriptionRecord)
	for _, record := range records {
		sdrs[record.PartitionID] = record
	}
	return &feeCreditTxRecorder{
		pdrs:               sdrs,
		state:              s,
		partitionID:        partitionID,
		transferFeeCredits: make(map[types.PartitionID][]*transferFeeCreditTx),
		reclaimFeeCredits:  make(map[types.PartitionID][]*reclaimFeeCreditTx),
	}
}

func (f *feeCreditTxRecorder) recordTransferFC(tx *transferFeeCreditTx) {
	sid := tx.attr.TargetPartitionID
	f.transferFeeCredits[sid] = append(f.transferFeeCredits[sid], tx)
}

func (f *feeCreditTxRecorder) recordReclaimFC(tx *reclaimFeeCreditTx) error {
	txo, err := tx.attr.CloseFeeCreditProof.TxRecord.GetTransactionOrderV1()
	if err != nil {
		return fmt.Errorf("failed to get transaction order: %w", err)
	}
	sid := txo.PartitionID
	f.reclaimFeeCredits[sid] = append(f.reclaimFeeCredits[sid], tx)
	return nil
}

func (f *feeCreditTxRecorder) getAddedCredit(sid types.PartitionID) uint64 {
	var sum uint64
	for _, transferFC := range f.transferFeeCredits[sid] {
		sum += transferFC.attr.Amount - transferFC.fee
	}
	return sum
}

func (f *feeCreditTxRecorder) getReclaimedCredit(sid types.PartitionID) uint64 {
	var sum uint64
	for _, reclaimFC := range f.reclaimFeeCredits[sid] {
		sum += reclaimFC.reclaimAmount
	}
	return sum
}

func (f *feeCreditTxRecorder) getSpentFeeSum() uint64 {
	var sum uint64
	for _, transferFCs := range f.transferFeeCredits {
		for _, transferFC := range transferFCs {
			sum += transferFC.fee
		}
	}
	for _, reclaimFCs := range f.reclaimFeeCredits {
		for _, reclaimFC := range reclaimFCs {
			sum += reclaimFC.reclaimFee
		}
	}
	return sum
}

func (f *feeCreditTxRecorder) reset() {
	clear(f.transferFeeCredits)
	clear(f.reclaimFeeCredits)
}

func (f *feeCreditTxRecorder) consolidateFees() error {
	// update fee credit bills for all known partitions with added and removed credits
	for sid, pdr := range f.pdrs {
		addedCredit := f.getAddedCredit(sid)
		reclaimedCredit := f.getReclaimedCredit(sid)
		if addedCredit == reclaimedCredit {
			continue // no update if bill value doesn't change
		}
		fcUnitID := pdr.FeeCreditBill.UnitID
		_, err := f.state.GetUnit(fcUnitID, false)
		if err != nil {
			return err
		}
		updateData := state.UpdateUnitData(fcUnitID,
			func(data types.UnitData) (types.UnitData, error) {
				bd, ok := data.(*money.BillData)
				if !ok {
					return nil, fmt.Errorf("unit %v does not contain bill data", fcUnitID)
				}
				bd.Value = bd.Value + addedCredit - reclaimedCredit
				return bd, nil
			})
		err = f.state.Apply(updateData)
		if err != nil {
			return fmt.Errorf("failed to update [%x] partition's fee credit bill: %w", pdr.PartitionID, err)
		}

		err = f.state.AddUnitLog(fcUnitID, nil)
		if err != nil {
			return fmt.Errorf("failed to update [%x] partition's fee credit bill state log: %w", pdr.PartitionID, err)
		}
	}

	// increment money fee credit bill with spent fees
	spentFeeSum := f.getSpentFeeSum()
	if spentFeeSum > 0 {
		moneyFCUnitID := f.pdrs[f.partitionID].FeeCreditBill.UnitID
		_, err := f.state.GetUnit(moneyFCUnitID, false)
		if err != nil {
			return fmt.Errorf("could not find money fee credit bill: %w", err)
		}
		updateData := state.UpdateUnitData(moneyFCUnitID,
			func(data types.UnitData) (types.UnitData, error) {
				bd, ok := data.(*money.BillData)
				if !ok {
					return nil, fmt.Errorf("unit %v does not contain bill data", moneyFCUnitID)
				}
				bd.Value = bd.Value + spentFeeSum
				return bd, nil
			})
		err = f.state.Apply(updateData)
		if err != nil {
			return fmt.Errorf("failed to update money fee credit bill with spent fees: %w", err)
		}

		err = f.state.AddUnitLog(moneyFCUnitID, nil)
		if err != nil {
			return fmt.Errorf("failed to update money fee credit bill state log: %w", err)
		}
	}
	return nil
}
