package rpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	testobservability "github.com/unicitynetwork/bft-core/internal/testutils/observability"
	"github.com/unicitynetwork/bft-core/internal/testutils/peer"
	"github.com/unicitynetwork/bft-go-base/types"
)

func TestGetNodeInfo_OK(t *testing.T) {
	node := &MockNode{}
	peerConf := peer.CreatePeerConfiguration(t)
	selfPeer := peer.CreatePeer(t, peerConf)
	api := NewAdminAPI(node, selfPeer, testobservability.Default(t))

	t.Run("ok", func(t *testing.T) {
		r, err := api.GetNodeInfo(context.Background())
		require.NoError(t, err)
		require.Equal(t, types.NetworkID(5), r.NetworkID)
		require.Equal(t, types.PartitionID(65536), r.PartitionID)
		require.Equal(t, types.PartitionTypeID(1), r.PartitionTypeID)
		require.Equal(t, selfPeer.ID().String(), r.Self.NodeID)
		require.Equal(t, selfPeer.MultiAddresses(), r.Self.Addresses)
		require.Empty(t, r.BootstrapNodes)
		require.Empty(t, r.RootValidators)
		require.Empty(t, r.PartitionValidators)
		require.Empty(t, r.OpenConnections)
	})
}
