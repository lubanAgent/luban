package luban

import (
	"fmt"
	"reflect"
)

// Request contains the messages for one Agent invocation and an optional
// typed result contract.
type Request struct {
	Messages []Message
	Output   *OutputSpec
}

// Message is one user or assistant message supplied to an Agent. The configured
// systemPrompt supplies the system role.
type Message struct {
	Role    Role
	Content []Part
}

// Role identifies a conversation message role.
type Role string

const (
	RoleUser      Role = "user"      // RoleUser marks a message from the user.
	RoleAssistant Role = "assistant" // RoleAssistant marks a message from the assistant.
)

// Part contains text or a URL-backed media item.
type Part struct {
	Type PartType
	Text string
	URL  string
}

// PartType identifies the content carried by a Part.
type PartType string

const (
	PartText  PartType = "text"  // PartText contains plain text.
	PartImage PartType = "image" // PartImage contains a URL-backed image.
	PartVideo PartType = "video" // PartVideo contains a URL-backed video.
	PartAudio PartType = "audio" // PartAudio contains a URL-backed audio item.
)

// OutputSpec describes the Go type an Agent must submit through its
// structured-output tool. Its fields remain private to keep the business API
// small.
type OutputSpec struct {
	goType          reflect.Type
	batchResultType reflect.Type
	makeBatchResult func([]batchItemValue) (any, error)
}

func (s *OutputSpec) valid() bool {
	return s != nil && s.goType != nil && s.makeBatchResult != nil
}

type batchItemValue struct {
	value any
	err   error
}

// Output declares the structured result type for a model or Agent invocation.
// When used by Batch, the returned spec can construct the actual named
// BatchResult[T] type without inferring T from a downstream function.
func Output[T any]() *OutputSpec {
	return &OutputSpec{
		goType:          reflect.TypeFor[T](),
		batchResultType: reflect.TypeFor[BatchResult[T]](),
		makeBatchResult: makeBatchResult[T],
	}
}

func makeBatchResult[T any](items []batchItemValue) (any, error) {
	result := BatchResult[T]{Items: make([]BatchItemResult[T], len(items))}
	for i, item := range items {
		if item.err != nil {
			result.Items[i].Err = item.err
			continue
		}
		if item.value == nil {
			if !isNilable(reflect.TypeFor[T]()) {
				return nil, fmt.Errorf("batch item %d has nil value for non-nilable output type %v", i, reflect.TypeFor[T]())
			}
			continue
		}

		value, ok := item.value.(T)
		if !ok {
			return nil, fmt.Errorf("batch item %d has value type %T, want %v", i, item.value, reflect.TypeFor[T]())
		}
		result.Items[i].Value = value
	}
	return result, nil
}

func isNilable(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}
