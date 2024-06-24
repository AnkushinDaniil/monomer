package localdb

import (
	"slices"
)

type dbBucket byte

const (
	bucketHeaderByHeight dbBucket = iota + 1
	bucketHeightByHash
	bucketLabelHeight
	bucketTxByHeightAndIndex
	bucketTxHeightAndIndexByHash
	bucketTxResultsByHeightAndIndex
)

// TODO: optimize the buckets with a buffer pool? We can improve type safety by using separate types for each bucket.
// https://github.com/golang/go/issues/23199#issuecomment-406967375

// Key flattens a prefix and series of byte arrays into a single []byte.
func (b dbBucket) Key(key ...[]byte) []byte {
	return append([]byte{byte(b)}, slices.Concat(key...)...)
}
