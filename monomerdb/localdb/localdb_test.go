package localdb_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/polymerdao/monomer/testutils"
	bfttypes "github.com/cometbft/cometbft/types"
	"github.com/polymerdao/monomer"
)

func TestAppendUnsafeBlock(t *testing.T) {
	db := testutils.NewLocalMemDB(t)
	header := &monomer.Header{}
	header.Hash()
	require.NoError(t, db.AppendBlock(&monomer.Block{
		Header: header,
		Txs: 
	}))
}
