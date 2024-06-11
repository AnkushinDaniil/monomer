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
)

var endian = binary.BigEndian

// Key flattens a prefix and series of byte arrays into a single []byte.
func (b dbBucket) Key(key ...[]byte) []byte {
	return append([]byte{byte(b)}, slices.Concat(key...)...)
}

// NOTE
// - There is an optimization here: we can use a cbor encoder to write directly to a batch, rather than copying bytes twice.
// - another optimization is to use a pool for dbBucket.Key

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
		// TODO index the transactions
		return nil
	})
}

func marshalUint64(x uint64) []byte {
	bytes := make([]byte, 8) //nolint:gomnd
	endian.PutUint64(bytes, x)
	return bytes
}

func (l *LocalDB) update(cb func(b *pebble.Batch) error) (err error) {
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

func (l *LocalDB) UpdateLabelHeight(label eth.BlockLabel, height uint64) (err error) {
	return l.update(func(b *pebble.Batch) error {
		return updateLabelHeight(b, label, marshalUint64(height))
	})
}

func updateLabelHeight(b *pebble.Batch, label eth.BlockLabel, heightBytes []byte) error {
	if err := b.Set(bucketLabelHeight.Key([]byte(label)), heightBytes, pebble.Sync); err != nil {
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

func (l *LocalDB) HeaderByHash(hash common.Hash) (_ *Header, err error) {
	s := l.db.NewSnapshot()
	defer func() {
		err = wrapCloseErr(err, s)
	}()
	heightBytes, closer, err := s.Get(bucketHeightByHash.Key(hash.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("get height by hash: %v", err)
	}
	defer func() {
		err = wrapCloseErr(err, closer)
	}()

	headerBytes, closer, err := s.Get(bucketHeaderByHeight.Key(heightBytes))
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

func (l *LocalDB) HeaderByLabel(label eth.BlockLabel) (_ *Header, err error) {
	s := l.db.NewSnapshot()
	defer func() {
		err = wrapCloseErr(err, s)
	}()
	heightBytes, closer, err := s.Get(bucketLabelHeight.Key([]byte(label)))
	if err != nil {
		return nil, fmt.Errorf("get height by hash: %v", err)
	}
	defer func() {
		err = wrapCloseErr(err, closer)
	}()

	h, err := headerByHeight(s, heightBytes)
	if err != nil {
		return nil, err
	}
	return h, nil
}

func (l *LocalDB) HeaderByHeight(uint64) (*Header, error) {
	panic("unimplemented")
}

func headerByHeight(s *pebble.Snapshot, heightBytes []byte) (_ *Header, err error) {
	headerBytes, closer, err := s.Get(bucketHeaderByHeight.Key(heightBytes))
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

func (l *LocalDB) MempoolLen() (uint64, error) {
	panic("unimplemented")
}
