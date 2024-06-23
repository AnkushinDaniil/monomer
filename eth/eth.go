package eth

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	bfttypes "github.com/cometbft/cometbft/types"
	rolluptypes "github.com/polymerdao/monomer/x/rollup/types"
	"github.com/polymerdao/monomer"
)

type ChainID struct {
	chainID *hexutil.Big
}

func NewChainID(chainID *hexutil.Big) *ChainID {
	return &ChainID{
		chainID: chainID,
	}
}

func (e *ChainID) ChainId() *hexutil.Big { //nolint:stylecheck
	return e.chainID
}

type headerStoreReader interface {
	blockIDReader
	HeaderAndTxsByHash(common.Hash) (*monomer.Header, bfttypes.Txs, error)
}

type Block struct {
	r headerStoreReader
}

func NewBlock(r headerStoreReader) *Block {
	return &Block{
		r: r,
	}
}

func (e *Block) GetBlockByNumber(id BlockID, inclTx bool) (map[string]any, error) {
	h, txs, err := id.Get(e.r)
	if err != nil {
		return nil, err
	}
	ethTxs, err := rolluptypes.AdaptCosmosTxsToEthTxs(txs)
	if err != nil {
		return nil, fmt.Errorf("adapt cosmos txs: %v", err)
	}
	return h.ToEthLikeBlock(ethTxs, inclTx), nil
}

func (e *Block) GetBlockByHash(hash common.Hash, inclTx bool) (map[string]any, error) {
	h, txs, err := e.r.HeaderAndTxsByHash(hash)
	if err != nil {
		return nil, fmt.Errorf("header and txs by hash: %v", err)
	}
	ethTxs, err := rolluptypes.AdaptCosmosTxsToEthTxs(txs)
	if err != nil {
		return nil, fmt.Errorf("adapt cosmos txs: %v", err)
	}
	return h.ToEthLikeBlock(ethTxs, inclTx), nil
}
