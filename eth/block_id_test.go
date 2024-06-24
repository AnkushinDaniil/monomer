package eth_test

import (
	"encoding/json"
	"fmt"
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	bfttypes "github.com/cometbft/cometbft/types"
	opeth "github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/polymerdao/monomer"
	"github.com/polymerdao/monomer/monomerdb"
	"github.com/polymerdao/monomer/eth"
	"github.com/polymerdao/monomer/testutils"
	"github.com/stretchr/testify/require"
)

func TestBlockIDUnmarshalValidJSON(t *testing.T) {
	tests := []struct {
		json string
		want eth.BlockID
	}{
		{
			json: "latest",
			want: eth.BlockID{
				Label: opeth.Unsafe,
			},
		},
		{
			json: "safe",
			want: eth.BlockID{
				Label: opeth.Safe,
			},
		},
		{
			json: "finalized",
			want: eth.BlockID{
				Label: opeth.Finalized,
			},
		},
		{
			json: "0x0",
			want: eth.BlockID{},
		},
		{
			json: "0x1",
			want: eth.BlockID{
				Height: 1,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.json, func(t *testing.T) {
			got := eth.BlockID{}
			require.NoError(t, json.Unmarshal([]byte(fmt.Sprintf("%q", test.json)), &got))
			require.Equal(t, test.want, got)
		})
	}
}

func TestBlockIDGet(t *testing.T) {
	blockStore := testutils.NewLocalMemDB(t)
	block := &monomer.Block{
		Header: &monomer.Header{
			HashCache: common.Hash{1},
		},
		Txs:     []bfttypes.Tx{{1}},
		Results: []*abcitypes.ExecTxResult{{}},
	}
	require.NoError(t, blockStore.AppendBlock(block))
	require.NoError(t, blockStore.UpdateLabel(opeth.Unsafe, block.Header.Hash()))

	tests := map[string]struct {
		id     eth.BlockID
		exists bool
	}{
		"unsafe exists": {
			id: eth.BlockID{
				Label: opeth.Unsafe,
			},
			exists: true,
		},
		"safe does not exist": {
			id: eth.BlockID{
				Label: opeth.Safe,
			},
		},
		"finalized does not exist": {
			id: eth.BlockID{
				Label: opeth.Finalized,
			},
		},
		"height 0 exists": {
			id: eth.BlockID{
				Height: 0,
			},
			exists: true,
		},
		"height 1 does not exist": {
			id: eth.BlockID{
				Height: 1,
			},
		},
	}

	for description, test := range tests {
		t.Run(description, func(t *testing.T) {
			h, txs, err := test.id.Get(blockStore)
			if test.exists {
				require.NoError(t, err)
				require.Equal(t, block.Txs, txs)
				require.Equal(t, block.Header, h)
			} else {
				require.ErrorContains(t, err, monomerdb.ErrNotFound.Error())
			}
		})
	}
}
