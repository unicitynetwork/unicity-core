package partition

import "github.com/unicitynetwork/bft-go-base/types"

/*
static orchestration until real thing is implemented.
*/
type Orchestration struct {
	trustBase types.RootTrustBase
}

func (orc Orchestration) TrustBase(epoch uint64) (types.RootTrustBase, error) {
	return orc.trustBase, nil
}
