package localdb_test

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	bfttypes "github.com/cometbft/cometbft/types"
	"github.com/polymerdao/monomer"
	"github.com/polymerdao/monomer/testapp"
	"github.com/polymerdao/monomer/testutils"
	"github.com/stretchr/testify/require"
)

func TestAppendUnsafeBlock(t *testing.T) {
	db := testutils.NewLocalMemDB(t)
	header := &monomer.Header{}
	header.Hash()
	require.NoError(t, db.AppendBlock(&monomer.Block{
		Header: header,
		Txs: bfttypes.ToTxs(testapp.ToTxs(t, map[string]string{
			"k": "v",
		})),
		Results: []*abcitypes.ExecTxResult{{}},
	}))
}
