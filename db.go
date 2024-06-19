package monomer

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
)

var ErrNotFound = errors.New("not found")

type Writer interface {
	AppendUnsafeBlock(*Block) error
	UpdateLabel(eth.BlockLabel, common.Hash) error
	Rollback(head, safe, finalized common.Hash) error

	MempoolEnqueue(bfttypes.Tx) error
	MempoolDequeue() (bfttypes.Tx, error)
}

type Reader interface {
	HeaderByHash(common.Hash) (*Header, error)
	HeaderByNumber(uint64) (*Header, error)
	HeaderByLabel(eth.BlockLabel) (*Header, error)

	MempoolLen() (uint64, error)
}

type LocalDB struct {
	db *pebble.DB
}

type dbBucket byte

const (
	bucketHeaderByHeight dbBucket = iota + 1
	bucketHeightByHash
	bucketLabelHeight
	bucketTxByHeightAndIndex
	bucketTxByHash
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

func (l *LocalDB) AppendUnsafeBlock(block *Block) (err error) {
	headerBytes, err := cbor.Marshal(block.Header)
	if err != nil {
		return fmt.Errorf("marshal cbor: %v", err)
	}
	heightBytes := marshalUint64(uint64(block.Header.Height))
	return l.update(func(b *pebble.Batch) (err error) {
		if err := b.Set(bucketHeaderByHeight.Key(heightBytes), headerBytes, nil); err != nil {
			return fmt.Errorf("set block by height: %v", err)
		}
		if err := b.Set(bucketHeightByHash.Key(block.Header.Hash.Bytes()), heightBytes, nil); err != nil {
			return fmt.Errorf("set height by hash: %v", err)
		}
		if err := updateLabelHeight(b, eth.Unsafe, heightBytes); err != nil {
			return err
		}

		//for i, tx := range block.Txs {
		//}
		// TODO index the transactions
		return nil
	})
}

func (l *LocalDB) UpdateLabelHeight(label eth.BlockLabel, height uint64) (err error) {
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

func (l *LocalDB) Rollback(head, safe, finalized common.Hash) error {
	panic("unimplemented")
}

func (l *LocalDB) MempoolEnqueue(bfttypes.Tx) error {
	panic("unimplemented")
}

func (l *LocalDB) MempoolDequeue() (bfttypes.Tx, error) {
	panic("unimplemented")
}

func (l *LocalDB) HeaderByHash(hash common.Hash) (*Header, error) {
	var header *Header
	if err := l.view(func(s *pebble.Snapshot) (err error) {
		heightBytes, closer, err := s.Get(bucketHeightByHash.Key(hash.Bytes()))
		if err != nil {
			return fmt.Errorf("get height by hash: %v", err)
		}
		defer func() {
			err = wrapCloseErr(err, closer)
		}()
		header, err = headerByHeight(s, heightBytes)
		return err
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func (l *LocalDB) HeaderByLabel(label eth.BlockLabel) (*Header, error) {
	var header *Header
	if err := l.view(func(s *pebble.Snapshot) (err error) {
		heightBytes, closer, err := s.Get(bucketLabelHeight.Key([]byte(label)))
		if err != nil {
			return fmt.Errorf("get height by hash: %v", err)
		}
		defer func() {
			err = wrapCloseErr(err, closer)
		}()
		header, err = headerByHeight(s, heightBytes)
		return err
	}); err != nil {
		return nil, err
	}
	return header, nil
}

func (l *LocalDB) HeaderByHeight(height uint64) (*Header, error) {
	return headerByHeight(l.db, marshalUint64(height))
}

func (l *LocalDB) MempoolLen() (uint64, error) {
	panic("unimplemented")
}

type getter interface {
	Get([]byte) ([]byte, io.Closer, error)
}

func headerByHeight(g getter, heightBytes []byte) (_ *Header, err error) {
	headerBytes, closer, err := g.Get(bucketHeaderByHeight.Key(heightBytes))
	if err != nil {
		return nil, fmt.Errorf("get header by height: %v", err)
	}
	defer func() {
		err = wrapCloseErr(err, closer)
	}()

	h := new(Header)
	if err := cbor.Unmarshal(headerBytes, &h); err != nil {
		return nil, fmt.Errorf("unmarshal cbor: %v", err)
	}
	return h, nil
}

func (l *LocalDB) view(cb func(*pebble.Snapshot) error) (err error) {
	s := l.db.NewSnapshot()
	defer func() {
		err = wrapCloseErr(err, s)
	}()
	return cb(s)
}

func (l *LocalDB) update(cb func(*pebble.Batch) error) (err error) {
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
		return multierror.Append(err, closer.Close())
	}
	return nil
}
