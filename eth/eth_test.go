package eth_test

import (
	"testing"

	abcitypes "github.com/cometbft/cometbft/abci/types"
	bfttypes "github.com/cometbft/cometbft/types"
	opeth "github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/polymerdao/monomer"
	"github.com/polymerdao/monomer/monomerdb"
	"github.com/polymerdao/monomer/eth"
	"github.com/polymerdao/monomer/testutils"
	rolluptypes "github.com/polymerdao/monomer/x/rollup/types"
	"github.com/stretchr/testify/require"
)

func TestChainId(t *testing.T) {
	for _, id := range []monomer.ChainID{0, 1, 2, 10} {
		t.Run(id.String(), func(t *testing.T) {
			hexID := id.HexBig()
			require.Equal(t, hexID, eth.NewChainID(hexID).ChainId())
		})
	}
}

func TestGetBlockByNumber(t *testing.T) {
	blockStore := testutils.NewLocalMemDB(t)
	require.NoError(t, blockStore.AppendHeaderAndTxs(&monomer.Header{
		HashCache: common.Hash{1},
	}), bfttypes.Txs([]bfttypes.Tx{{1}}))

	tests := map[string]struct {
		id   eth.BlockID
		want *monomer.Block
	}{
		"unsafe block exists": {
			id: eth.BlockID{
				Label: opeth.Unsafe,
			},
			want: block,
		},
		"safe block does not exist": {
			id: eth.BlockID{
				Label: opeth.Safe,
			},
		},
		"finalized block does not exist": {
			id: eth.BlockID{
				Label: opeth.Finalized,
			},
		},
		"block 0 exists": {
			id:   eth.BlockID{},
			want: block,
		},
		"block 1 does not exist": {
			id: eth.BlockID{
				Height: 1,
			},
		},
	}

	for description, test := range tests {
		t.Run(description, func(t *testing.T) {
			for description, includeTxs := range map[string]bool{
				"include txs": true,
				"exclude txs": false,
			} {
				t.Run(description, func(t *testing.T) {
					s := eth.NewBlock(blockStore)
					got, err := s.GetBlockByNumber(test.id, includeTxs)
					if test.want == nil {
						require.ErrorContains(t, err, monomerdb.ErrNotFound.Error())
						require.Nil(t, got)
						return
					}
					require.NoError(t, err)
					require.Equal(t, test.want.Header.ToEthLikeBlock(ethTxs(t, block.Txs), includeTxs), got)
				})
			}
		})
	}
}

func TestGetBlockByHash(t *testing.T) {
	blockStore := testutils.NewPebbleMemDB(t)
	block := &monomer.Block{
		Header: &monomer.Header{
			HashCache: common.Hash{1},
		},
		Txs:     bfttypes.Txs([]bfttypes.Tx{{1}}),
		Results: []*abcitypes.ExecTxResult{{}},
	}
	require.NoError(t, blockStore.AppendUnsafeBlock(block))

	for description, inclTx := range map[string]bool{
		"include txs": true,
		"exclude txs": false,
	} {
		t.Run(description, func(t *testing.T) {
			t.Run("block hash 1 exists", func(t *testing.T) {
				e := eth.NewBlock(blockStore)
				got, err := e.GetBlockByHash(block.Header.HashCache, inclTx)
				require.NoError(t, err)
				require.Equal(t, block.Header.ToEthLikeBlock(ethTxs(t, block.Txs), inclTx), got)
			})
		})
		t.Run("block hash 0 does not exist", func(t *testing.T) {
			for description, inclTx := range map[string]bool{
				"include txs": true,
				"exclude txs": false,
			} {
				t.Run(description, func(t *testing.T) {
					e := eth.NewBlock(blockStore)
					got, err := e.GetBlockByHash(common.Hash{}, inclTx)
					require.Nil(t, got)
					require.ErrorContains(t, err, monomerdb.ErrNotFound.Error())
				})
			}
		})
	}
}

func ethTxs(t *testing.T, cosmosTxs bfttypes.Txs) ethtypes.Transactions {
	txs, err := rolluptypes.AdaptCosmosTxsToEthTxs(cosmosTxs)
	require.NoError(t, err)
	return txs
}
