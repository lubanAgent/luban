package luban

import (
	"errors"
	"reflect"
	"testing"
)

type testReview struct {
	Score int
}

func TestOutputCapturesNamedBatchResultTypeAndFactory(t *testing.T) {
	spec := Output[testReview]()
	if spec.goType != reflect.TypeFor[testReview]() {
		t.Fatalf("single output type = %v", spec.goType)
	}
	if spec.batchResultType != reflect.TypeFor[BatchResult[testReview]]() {
		t.Fatalf("batch result type = %v", spec.batchResultType)
	}

	itemErr := errors.New("item failed")
	got, err := spec.makeBatchResult([]batchItemValue{
		{value: testReview{Score: 8}},
		{err: itemErr},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, ok := got.(BatchResult[testReview])
	if !ok {
		t.Fatalf("factory returned %T", got)
	}
	if len(result.Items) != 2 || result.Items[0].Value.Score != 8 || result.Items[0].Err != nil {
		t.Fatalf("unexpected successful item: %+v", result.Items)
	}
	if !errors.Is(result.Items[1].Err, itemErr) {
		t.Fatalf("item error chain was not preserved: %v", result.Items[1].Err)
	}
}

func TestBatchFactoryPreservesNilAndRejectsMismatchedValue(t *testing.T) {
	spec := Output[*testReview]()
	got, err := spec.makeBatchResult([]batchItemValue{{value: nil}})
	if err != nil {
		t.Fatal(err)
	}
	result := got.(BatchResult[*testReview])
	if result.Items[0].Value != nil {
		t.Fatalf("nil pointer value changed: %+v", result.Items[0])
	}

	if _, err := Output[testReview]().makeBatchResult([]batchItemValue{{value: "wrong"}}); err == nil {
		t.Fatal("expected an incompatible item value to return an error")
	}
	if _, err := Output[int]().makeBatchResult([]batchItemValue{{value: nil}}); err == nil {
		t.Fatal("expected nil for a non-nilable value type to return an error")
	}
}
