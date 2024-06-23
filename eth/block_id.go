package eth

import (
	"encoding/json"
	"fmt"

	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/polymerdao/monomer"
	bfttypes "github.com/cometbft/cometbft/types"
)

type BlockID struct {
	Label  eth.BlockLabel
	Height uint64
}

func (id *BlockID) UnmarshalJSON(data []byte) error {
	var dataStr string
	if err := json.Unmarshal(data, &dataStr); err != nil {
		return fmt.Errorf("unmarshal block id into string: %v", err)
	}

	switch dataStr {
	case eth.Unsafe, eth.Safe, eth.Finalized:
		id.Label = eth.BlockLabel(dataStr)
	default:
		var height hexutil.Uint64
		if err := height.UnmarshalText([]byte(dataStr)); err != nil {
			return fmt.Errorf("unmarshal height as hexutil.Uint64: %v", err)
		}
		id.Height = uint64(height)
	}
	return nil
}

type blockIDReader interface {
	HeaderAndTxsByLabel(eth.BlockLabel) (*monomer.Header, bfttypes.Txs, error)
	HeaderAndTxsByHeight(uint64) (*monomer.Header, bfttypes.Txs, error)
}

func (id *BlockID) Get(r blockIDReader) (*monomer.Header, bfttypes.Txs, error) {
	if id.Label != "" {
		h, txs, err := r.HeaderAndTxsByLabel(id.Label)
		if err != nil {
			return nil, nil, fmt.Errorf("get header by label: %v", err)
		}
		return h, txs, nil
	}
	h, txs, err := r.HeaderAndTxsByHeight(id.Height)
	if err != nil {
		return nil, nil, fmt.Errorf("get header by height: %v", err)
	}
	return h, txs, nil
}
