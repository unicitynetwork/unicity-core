package consensus

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/unicitynetwork/bft-core/logger"
	drctypes "github.com/unicitynetwork/bft-core/rootchain/consensus/types"
	"github.com/unicitynetwork/bft-go-base/types"
)

type (
	IRChangeVerifier interface {
		VerifyIRChangeReq(round uint64, irChReq *drctypes.IRChangeReq) (*types.InputRecord, error)
	}
	PartitionTimeout interface {
		GetT2Timeouts(currenRound uint64) ([]types.PartitionID, error)
	}
	irChange struct {
		InputRecord *types.InputRecord
		Reason      drctypes.IRChangeReason
		Req         *drctypes.IRChangeReq
	}
	IrReqBuffer struct {
		irChgReqBuffer map[types.PartitionShardID]*irChange
		log            *slog.Logger
	}

	InProgressFn func(partition types.PartitionID, shard types.ShardID) *types.InputRecord
)

func NewIrReqBuffer(log *slog.Logger) *IrReqBuffer {
	return &IrReqBuffer{
		irChgReqBuffer: make(map[types.PartitionShardID]*irChange),
		log:            log,
	}
}

// Add validates incoming IR change request and buffers valid requests. If for any reason the IR request is found not
// valid, reason is logged, error is returned and request is ignored.
func (x *IrReqBuffer) Add(round uint64, irChReq *drctypes.IRChangeReq, ver IRChangeVerifier) error {
	if irChReq == nil {
		return errors.New("ir change request is nil")
	}
	// special case, timeout cannot be requested, it can only be added to a block by the leader
	if irChReq.CertReason == drctypes.T2Timeout {
		return errors.New("invalid ir change request, timeout can only be proposed by leader issuing a new block")
	}
	ir, err := ver.VerifyIRChangeReq(round, irChReq)
	if err != nil {
		return fmt.Errorf("ir change request verification: %w", err)
	}

	psID := types.PartitionShardID{PartitionID: irChReq.Partition, ShardID: irChReq.Shard.Key()}

	// verify and extract proposed IR, NB! in this case we set the age to 0 as
	// currently no request can be received to request timeout
	newIrChReq := &irChange{InputRecord: ir, Reason: irChReq.CertReason, Req: irChReq}
	if irChangeReq, found := x.irChgReqBuffer[psID]; found {
		if irChangeReq.Reason != newIrChReq.Reason {
			return fmt.Errorf("equivocating request for partition %s, reason has changed", psID.PartitionID)
		}
		if b, err := types.EqualIR(irChangeReq.InputRecord, newIrChReq.InputRecord); b || err != nil {
			if err != nil {
				return fmt.Errorf("failed to compare IRs: %w", err)
			}
			// duplicate already stored
			x.log.Debug("duplicate IR change request, ignored", logger.Shard(irChReq.Partition, irChReq.Shard))
			return nil
		}
		// At this point it is not possible to cast blame, so just return error and ignore
		return fmt.Errorf("equivocating request for partition %s-%s", irChReq.Partition, irChReq.Shard)
	}
	// Insert first valid request received and compare the others received against it
	x.irChgReqBuffer[psID] = newIrChReq
	return nil
}

// IsChangeInBuffer returns true if there is a request for IR change from the partition
// in the buffer
func (x *IrReqBuffer) IsChangeInBuffer(partitionID types.PartitionID, shardID types.ShardID) bool {
	psID := types.PartitionShardID{PartitionID: partitionID, ShardID: shardID.Key()}
	_, found := x.irChgReqBuffer[psID]
	return found
}

// GeneratePayload generates new proposal payload from buffered IR change requests.
func (x *IrReqBuffer) GeneratePayload(round uint64, timeouts []*types.UnicityCertificate, inProgress InProgressFn) *drctypes.Payload {
	payload := &drctypes.Payload{
		Requests: make([]*drctypes.IRChangeReq, 0, len(x.irChgReqBuffer)+len(timeouts)),
	}
	// first add timeout requests
	for _, uc := range timeouts {
		pID := uc.GetPartitionID()
		sID := uc.GetShardID()
		// if there is a request for the same partition (same id) in buffer (prefer progress to timeout) or
		// if there is a change already in the pipeline for this partition id
		if x.IsChangeInBuffer(pID, sID) || inProgress(pID, sID) != nil {
			x.log.Debug(fmt.Sprintf("T2 timeout request ignored, partition %s has pending change in progress", pID),
				logger.Shard(pID, sID))
			continue
		}
		x.log.Debug(fmt.Sprintf("partition %s request T2 timeout", pID), logger.Shard(pID, sID))
		payload.Requests = append(payload.Requests, &drctypes.IRChangeReq{
			Partition:  pID,
			Shard:      sID,
			CertReason: drctypes.T2Timeout,
		})
	}
	for _, req := range x.irChgReqBuffer {
		if inProgress(req.Req.Partition, req.Req.Shard) != nil {
			// if there is a pending block with the partition id in progress then do not propose a change
			// before last has been certified
			x.log.Debug(fmt.Sprintf("partition %s request ignored, pending change in pipeline", req.Req.Partition), logger.Shard(req.Req.Partition, req.Req.Shard))
			continue
		}
		payload.Requests = append(payload.Requests, req.Req)
	}
	// clear the buffer once payload is done
	clear(x.irChgReqBuffer)
	return payload
}
