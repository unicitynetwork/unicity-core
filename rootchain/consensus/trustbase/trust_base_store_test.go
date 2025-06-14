package trustbase

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/unicitynetwork/bft-core/internal/testutils/trustbase"
	"github.com/unicitynetwork/bft-core/keyvaluedb/memorydb"
	"github.com/unicitynetwork/bft-go-base/crypto"
	"github.com/unicitynetwork/bft-go-base/types"
)

func TestTrustBaseStore(t *testing.T) {
	// create db
	db, err := memorydb.New()
	require.NoError(t, err)
	trustBaseStore, err := NewStore(db)
	require.NoError(t, err)
	require.Equal(t, db, trustBaseStore.GetDB())

	// load trust base from empty store
	tb, err := trustBaseStore.LoadTrustBase(0)
	require.NoError(t, err)
	require.Nil(t, tb)

	// create trust base
	signer, err := crypto.NewInMemorySecp256K1Signer()
	require.NoError(t, err)
	verifier, err := signer.Verifier()
	require.NoError(t, err)
	tb, err = types.NewTrustBaseGenesis(
		5,
		[]*types.NodeInfo{trustbase.NewNodeInfoFromVerifier(t, "test", verifier)},
	)
	require.NoError(t, err)

	// store trust base
	err = trustBaseStore.StoreTrustBase(0, tb)
	require.NoError(t, err)

	// verify trust base can be loaded
	tbFromDB, err := trustBaseStore.LoadTrustBase(0)
	require.NoError(t, err)
	require.Equal(t, tb, tbFromDB)
}
