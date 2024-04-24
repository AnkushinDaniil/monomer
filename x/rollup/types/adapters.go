package types

import (
	"errors"
	"fmt"
	"slices"

	bfttypes "github.com/cometbft/cometbft/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/ethereum/go-ethereum/common/hexutil"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
)

func AdaptPayloadTxsToCosmosTxs(ethTxs []hexutil.Bytes) (bfttypes.Txs, error) {
	if len(ethTxs) == 0 {
		return bfttypes.Txs{}, nil
	}

	// Pack deposit txs into a single sdk.Msg.
	var depositTxs [][]byte
	var cosmosTxs [][]byte
	for _, txBytes := range ethTxs {
		var tx ethtypes.Transaction
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			return nil, fmt.Errorf("unmarshal tx binary: %v", err)
		}
		if tx.IsDepositTx() {
			depositTxs = append(depositTxs, txBytes)
		} else {
			cosmosTxs = append(cosmosTxs, tx.Data())
		}
	}

	// Pack deposit txs into Cosmos tx.
	msgAny, err := codectypes.NewAnyWithValue(&MsgL1Txs{
		TxBytes: depositTxs,
	})
	if err != nil {
		return nil, fmt.Errorf("new any with value: %v", err)
	}
	sdkDepositTxBytes, err := (&sdktx.Tx{
		Body: &sdktx.TxBody{
			Messages: []*codectypes.Any{msgAny},
		},
	}).Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal tx: %v", err)
	}

	return bfttypes.ToTxs(slices.Insert(cosmosTxs, 0, sdkDepositTxBytes)), nil
}

func AdaptCosmosTxsToEthTxs(cosmosTxs bfttypes.Txs) (ethtypes.Transactions, error) {
	if cosmosTxs.Len() == 0 {
		return ethtypes.Transactions{}, nil
	}
	txsBytes := cosmosTxs.ToSliceOfBytes()

	var txs ethtypes.Transactions

	// Unpack deposits from the MsgL1Txs msg.
	cosmosEthTxBytes := txsBytes[0]
	cosmosEthTx := new(sdktx.Tx)
	if err := cosmosEthTx.Unmarshal(cosmosEthTxBytes); err != nil {
		return nil, fmt.Errorf("unmarshal cosmos tx: %v", err)
	}
	msgs := cosmosEthTx.GetBody().GetMessages()
	if num := len(msgs); num != 1 {
		return nil, fmt.Errorf("unexpected number of msgs in Eth Cosmos tx: want 1, got %d", num)
	}
	msg := new(MsgL1Txs)
	if err := msg.Unmarshal(msgs[0].GetValue()); err != nil {
		return nil, fmt.Errorf("unmarshal MsgL1Txs smsg: %v", err)
	}
	ethTxsBytes := msg.GetTxBytes()
	if len(ethTxsBytes) == 0 {
		return nil, errors.New("L1 Attributes tx not found")
	}
	for _, txBytes := range ethTxsBytes {
		var tx ethtypes.Transaction
		if err := tx.UnmarshalBinary(txBytes); err != nil {
			return nil, fmt.Errorf("unmarshal tx binary: %v", err)
		}
		if !tx.IsDepositTx() {
			return nil, errors.New("MsgL1Tx contains non-deposit tx")
		}
		txs = append(txs, &tx)
	}

	// Pack Cosmos txs into Ethereum txs.
	for _, txBytes := range txsBytes[1:] {
		txs = append(txs, ethtypes.NewTx(&ethtypes.DynamicFeeTx{
			// TODO maybe fill in other fields?
			Data: txBytes,
		}))
	}

	return txs, nil
}
