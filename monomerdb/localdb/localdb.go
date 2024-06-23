package localdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/cockroachdb/pebble"
	"github.com/polymerdao/monomer"
	bfttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/hashicorp/go-multierror"
)

var ErrNotFound = errors.New("not found")

type DB struct {
	db *pebble.DB
}

func New(db *pebble.DB) *DB {
	return &DB{
		db: db,
	}
}

type dbBucket byte

const (
	bucketHeaderByHeight dbBucket = iota + 1
	bucketHeightByHash
	bucketLabelHeight
	bucketTxByHeightAndIndex
	bucketTxHeightAndIndexByHash
	bucketTxResultsByHeightAndIndex
)

var endian = binary.BigEndian

// Key flattens a prefix and series of byte arrays into a single []byte.
func (b dbBucket) Key(key ...[]byte) []byte {
	return append([]byte{byte(b)}, slices.Concat(key...)...)
}

// NOTE
// - There is an optimization here: we can use a cbor encoder to write directly to a batch, rather than copying bytes twice.
// - another optimization is to use a pool for dbBucket.Key.
//   - https://github.com/golang/go/issues/23199#issuecomment-406967375
// - We can also make bucket keys more type safe by using separate types for each bucket.

// AppendUnsafeBlock does no validity checks on the block.
func (l *DB) AppendUnsafeBlock(block *monomer.Block) error {
	headerBytes, err := cbor.Marshal(block.Header)
	if err != nil {
		return fmt.Errorf("marshal header cbor: %v", err)
	}
	heightBytes := marshalUint64(uint64(block.Header.Height))
	return l.update(func(b *pebble.Batch) error {
		if err := b.Set(bucketHeaderByHeight.Key(heightBytes), headerBytes, nil); err != nil {
			return fmt.Errorf("set block by height: %v", err)
		}
		if err := b.Set(bucketHeightByHash.Key(block.Header.HashCache.Bytes()), heightBytes, nil); err != nil {
			return fmt.Errorf("set height by hash: %v", err)
		}
		if err := updateLabelHeight(b, eth.Unsafe, heightBytes); err != nil {
			return err
		}

		for i, tx := range block.Txs {
			heightAndIndexBytes := slices.Concat(heightBytes, marshalUint64(uint64(i)))
			if err := b.Set(bucketTxByHeightAndIndex.Key(heightAndIndexBytes), tx, nil); err != nil {
				return fmt.Errorf("set tx by height and index: %v", err)
			}
			if err := b.Set(bucketTxHeightAndIndexByHash.Key(tx.Hash()), heightAndIndexBytes, nil); err != nil {
				return fmt.Errorf("set tx height and index by hash: %v", err)
			}

			txResultBytes, err := block.Results[i].Marshal()
			if err != nil {
				return fmt.Errorf("marshal tx result: %v", err)
			}
			if err := b.Set(bucketTxResultsByHeightAndIndex.Key(heightAndIndexBytes), txResultBytes, nil); err != nil {
				return fmt.Errorf("set tx result by height and index: %v", err)
			}
		}
		return nil
	})
}

func (l *DB) UpdateLabelHeight(label eth.BlockLabel, height uint64) (err error) {
	return updateLabelHeight(l.db, label, marshalUint64(height))
}

type setter interface {
	Set([]byte, []byte, *pebble.WriteOptions) error
}

func updateLabelHeight(s setter, label eth.BlockLabel, heightBytes []byte) error {
	if err := s.Set(bucketLabelHeight.Key([]byte(label)), heightBytes, pebble.Sync); err != nil {
		return fmt.Errorf("set label %q height: %v", label, err)
	}
	return nil
}

func (l *DB) RollbackToHeight(head, safe, finalized common.Hash) error {
	panic("unimplemented")
}

func (l *DB) MempoolEnqueue(bfttypes.Tx) error {
	panic("unimplemented")
}

func (l *DB) MempoolDequeue() (bfttypes.Tx, error) {
	panic("unimplemented")
}

func (l *DB) HeaderAndTxsByHeight(height uint64) (*monomer.Header, bfttypes.Txs, error) {
	var header *monomer.Header
	var txs bfttypes.Txs
	if err := l.view(func(s *pebble.Snapshot) (err error) {
		heightBytes := marshalUint64(height)
		header, err = headerByHeight(s, heightBytes)
		if err != nil {
			return fmt.Errorf("get header by height: %v", err)
		}
		txs, err = txsInRange(s, heightBytes, marshalUint64(height+1))
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return header, txs, nil
}

// TODO wrap errors properly

func (l *DB) HeaderAndTxsByHash(hash common.Hash) (*monomer.Header, bfttypes.Txs, error) {
	var header *monomer.Header
	var txs bfttypes.Txs
	if err := l.view(func(s *pebble.Snapshot) (err error) {
		header, err = headerByHash(s, hash)
		if err != nil {
			return fmt.Errorf("get header by hash: %w", err)
		}
		txs, err = txsInRange(s, marshalUint64(uint64(header.Height)), marshalUint64(uint64(header.Height + 1)))
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return header, txs, nil
}

func (l *DB) HeaderAndTxsByLabel(label eth.BlockLabel) (*monomer.Header, bfttypes.Txs, error) {
	var header *monomer.Header
	var txs bfttypes.Txs
	if err := l.view(func(s *pebble.Snapshot) (err error) {
		header, err = headerByLabel(s, label)
		if err != nil {
			return fmt.Errorf("get header by label: %v", err)
		}
		txs, err = txsInRange(s, marshalUint64(uint64(header.Height)), marshalUint64(uint64(header.Height + 1)))
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return header, txs, nil
}

func txsInRange(s *pebble.Snapshot, startHeightBytes, endHeightBytes []byte) (_ bfttypes.Txs, err error) {
	iter, err := s.NewIter(&pebble.IterOptions{
		LowerBound: bucketTxByHeightAndIndex.Key(startHeightBytes),
		UpperBound: bucketTxByHeightAndIndex.Key(endHeightBytes),
	})
	if err != nil {
		return nil, fmt.Errorf("new iterator: %v", err)
	}
	defer func() {
		err = wrapCloseErr(err, iter)
	}()
	var txs bfttypes.Txs
	for ; iter.Valid(); iter.Next() {
		value, err := iter.ValueAndErr()
		if err != nil {
			return nil, fmt.Errorf("get value from iterator: %v", err)
		}
		var tx bfttypes.Tx
		copy(tx, value)
		txs = append(txs, tx)
	}
	return txs, nil
}

func (l *DB) MempoolLen() (uint64, error) {
	panic("unimplemented")
}

type getter interface {
	Get([]byte) ([]byte, io.Closer, error)
}

func headerByHeight(g getter, heightBytes []byte) (_ *monomer.Header, err error) {
	headerBytes, closer, err := get(g, bucketHeaderByHeight.Key(heightBytes))
	if err != nil {
		return nil, err
	}
	defer func() {
		err = wrapCloseErr(err, closer)
	}()

	h := new(monomer.Header)
	if err := cbor.Unmarshal(headerBytes, &h); err != nil {
		return nil, fmt.Errorf("unmarshal cbor: %v", err)
	}
	return h, nil
}

func headerByLabel(s *pebble.Snapshot, label eth.BlockLabel) (_ *monomer.Header, err error) {
	heightBytes, closer, err := get(s, bucketLabelHeight.Key([]byte(label)))
	if err != nil {
		return nil, fmt.Errorf("get label height: %v", err)
	}
	defer func() {
		err = wrapCloseErr(err, closer)
	}()
	header, err := headerByHeight(s, heightBytes)
	if err != nil {
		return nil, fmt.Errorf("header by height: %v", err)
	}
	return header, nil
}

func headerByHash(s *pebble.Snapshot, hash common.Hash) (_ *monomer.Header, err error) {
	heightBytes, closer, err := get(s, bucketHeightByHash.Key(hash.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("get height by hash: %w", err)
	}
	defer func() {
		err = wrapCloseErr(err, closer)
	}()
	header, err := headerByHeight(s, heightBytes)
	if err != nil {
		return nil, fmt.Errorf("header by height: %w", err)
	}
	return header, nil
}

func (l *DB) view(cb func(*pebble.Snapshot) error) (err error) {
	s := l.db.NewSnapshot()
	defer func() {
		err = wrapCloseErr(err, s)
	}()
	return cb(s)
}

func (l *DB) update(cb func(*pebble.Batch) error) (err error) {
	b := l.db.NewBatch()
	defer func() {
		err = wrapCloseErr(err, b)
	}()
	if err := cb(b); err != nil {
		return err
	}
	if err := b.Commit(pebble.Sync); err != nil {
		return fmt.Errorf("commit: %v", err)
	}
	return nil
}

func get(g getter, key []byte) (_ []byte, _ io.Closer, err error) {
	value, closer, err := g.Get(key)
	if err != nil {
		if errors.Is(err, pebble.ErrNotFound) {
			return nil, nil, ErrNotFound
		}
		return nil, nil, fmt.Errorf("%v", err) // Obfuscate the error.
	}
	return value, closer, nil
}

func marshalUint64(x uint64) []byte {
	bytes := make([]byte, 8) //nolint:gomnd
	endian.PutUint64(bytes, x)
	return bytes
}

func wrapCloseErr(err error, closer io.Closer) error {
	closeErr := closer.Close()
	if closeErr != nil {
		closeErr = fmt.Errorf("close: %v", closeErr)
	}
	if err != nil || closeErr != nil {
		return multierror.Append(err, closeErr)
	}
	return nil
}
