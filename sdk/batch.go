package luban

import "errors"

// Batch contains independent model inputs processed with a per-batch
// concurrency limit.
type Batch struct {
	Concurrency int
	Output      *OutputSpec
	Items       []BatchItem
}

// BatchItem contains the messages for one batch invocation.
type BatchItem struct {
	Messages []Message
}

// BatchResult preserves the input order and the success or error of every
// item.
type BatchResult[T any] struct {
	Items []BatchItemResult[T]
}

// BatchItemResult contains one result or its item-specific error.
type BatchItemResult[T any] struct {
	Value T
	Err   error
}

// ErrEmptyBatch is returned when a batch has no items.
var ErrEmptyBatch = errors.New("luban: empty batch")
