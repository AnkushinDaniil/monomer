package localdb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/cockroachdb/pebble"
	bfttypes "github.com/cometbft/cometbft/types"
	"github.com/ethereum-optimism/optimism/op-service/eth"
	"github.com/ethereum/go-ethereum/common"
	"github.com/fxamacker/cbor/v2"
	"github.com/hashicorp/go-multierror"
	"github.com/polymerdao/monomer"
)

var (
	ErrNotFound = errors.New("not found")

	endian = binary.BigEndian
)

type DB struct {
	db *pebble.DB
}

func New(db *pebble.DB) *DB {
	return &DB{
		db: db,
	}
}

// TODO: optimization - we can use a cbor encoder to write directly to a batch, rather than copying bytes twice.
// TODO wrap errors properly

// AppendHeaderAndTxs does no validity checks on the block and does not update labels.
func (db *DB) AppendHeaderAndTxs(header *monomer.Header, txs bfttypes.Txs) error {
	headerBytes, err := cbor.Marshal(header)
	if err != nil {
		return fmt.Errorf("marshal header cbor: %v", err)
	}
	heightBytes := marshalUint64(uint64(header.Height))
	return db.update(func(b *pebble.Batch) error {
		if err := b.Set(bucketHeaderByHeight.Key(heightBytes), headerBytes, nil); err != nil {
			return fmt.Errorf("set block by height: %v", err)
		}
		if err := b.Set(bucketHeightByHash.Key(header.HashCache.Bytes()), heightBytes, nil); err != nil {
			return fmt.Errorf("set height by hash: %v", err)
		}

		for i, tx := range txs {
			heightAndIndexBytes := slices.Concat(heightBytes, marshalUint64(uint64(i)))
			if err := b.Set(bucketTxByHeightAndIndex.Key(heightAndIndexBytes), tx, nil); err != nil {
				return fmt.Errorf("set tx by height and index: %v", err)
			}
			if err := b.Set(bucketTxHeightAndIndexByHash.Key(tx.Hash()), heightAndIndexBytes, nil); err != nil {
				return fmt.Errorf("set tx height and index by hash: %v", err)
			}
		}
		return nil
	})
}

func (db *DB) UpdateLabel(label eth.BlockLabel, hash common.Hash) error {
	if err := db.db.Set(bucketLabelHash.Key([]byte(label)), hash.Bytes(), pebble.Sync); err != nil {
		return fmt.Errorf("%s: %v", label, err)
	}
	return nil
}

func (db *DB) Rollback(unsafe, safe, finalized common.Hash) error {
	return db.updateIndexed(func(b *pebble.Batch) error {
		unsafeHeightBytesValue, closer, err := get(b, bucketHeightByHash.Key(unsafe.Bytes()))
		if err != nil {
			return fmt.Errorf("get height by hash: %w", err)
		}
		unsafeHeight := endian.Uint64(unsafeHeightBytesValue)
		if err := closer.Close(); err != nil {
			return fmt.Errorf("close unsafeHeightBytesValue closer: %v", err)
		}

		// iterator to get hashes of removed headers

		firstHeightBytesToDelete := marshalUint64(unsafeHeight + 1)
		if err := b.DeleteRange(bucketHeaderByHeight.Key(firstHeightBytesToDelete), marshalUint64(uint64(bucketHeaderByHeight) + 1), nil); err != nil {
			return fmt.Errorf("delete range of headers: %v", err)
		}

		// iterator to get hashes of removed txs

		if err := b.DeleteRange(bucketTxHeightAndIndexByHash.Key(firstHeightBytesToDelete), marshalUint64(uint64(bucketTxHeightAndIndexByHash)+1), nil); err != nil {
			return fmt.Errorf("delete range of txs: %v", err)
		}

		// update labels
		return nil
	})
}

func (db *DB) HeaderAndTxsByHeight(height uint64) (*monomer.Header, bfttypes.Txs, error) {
	var header *monomer.Header
	var txs bfttypes.Txs
	if err := db.view(func(s *pebble.Snapshot) (err error) {
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

func (db *DB) HeaderAndTxsByHash(hash common.Hash) (*monomer.Header, bfttypes.Txs, error) {
	var header *monomer.Header
	var txs bfttypes.Txs
	if err := db.view(func(s *pebble.Snapshot) (err error) {
		header, err = headerByHash(s, hash)
		if err != nil {
			return fmt.Errorf("get header by hash: %w", err)
		}
		txs, err = txsInRange(s, marshalUint64(uint64(header.Height)), marshalUint64(uint64(header.Height+1)))
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return header, txs, nil
}

func (db *DB) HeaderAndTxsByLabel(label eth.BlockLabel) (*monomer.Header, bfttypes.Txs, error) {
	var header *monomer.Header
	var txs bfttypes.Txs
	if err := db.view(func(s *pebble.Snapshot) error {
		header, err := headerByLabel(s, label)
		if err != nil {
			return fmt.Errorf("get header by label: %v", err)
		}
		txs, err = txsInRange(s, marshalUint64(uint64(header.Height)), marshalUint64(uint64(header.Height+1)))
		if err != nil {
			return err
		}
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return header, txs, nil
}

func (db *DB) HeaderByHash(hash common.Hash) (*monomer.Header, error) {
	var header *monomer.Header
	if err := db.view(func(s *pebble.Snapshot) error {
		var err error
		header, err = headerByHash(s, hash)
		return err
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func (db *DB) HeaderByLabel(label eth.BlockLabel) (*monomer.Header, error) {
	var header *monomer.Header
	if err := db.view(func(s *pebble.Snapshot) error {
		var err error
		header, err = headerByLabel(s, label)
		return err
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func (db *DB) HeaderByHeight(height uint64) (*monomer.Header, error) {
	return headerByHeight(db.db, marshalUint64(height))
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
	for iter.First(); iter.Valid(); iter.Next() {
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
	heightBytes, closer, err := get(s, bucketLabelHash.Key([]byte(label)))
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

func (l *DB) updateIndexed(cb func(*pebble.Batch) error) (err error) {
	b := l.db.NewIndexedBatch()
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
