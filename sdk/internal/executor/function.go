// Package executor contains reflection-based business function analysis and
// invocation. It deliberately has no dependency on the public luban package.
package executor

import (
	"fmt"
	"reflect"

	"github.com/gin-gonic/gin"
)

var (
	ginContextType = reflect.TypeOf((*gin.Context)(nil))
	errorType      = reflect.TypeOf((*error)(nil)).Elem()
)

// Function is the immutable signature metadata for a registered function.
type Function struct {
	Value      reflect.Value
	InputType  reflect.Type
	OutputType reflect.Type
	HasInput   bool
	HasOutput  bool
	HasError   bool
}

// Result represents one function's business output. HasValue distinguishes
// “no business output” from an actual nil value returned through any.
type Result struct {
	Value    any
	HasValue bool
}

// Inspect validates the public function contract and captures its signature.
func Inspect(key string, fn any) (Function, error) {
	if isNilInterface(fn) {
		return Function{}, fmt.Errorf("luban: function %q is nil", key)
	}
	value := reflect.ValueOf(fn)
	typeOf := value.Type()
	if typeOf.Kind() != reflect.Func {
		return Function{}, fmt.Errorf("luban: function %q has non-function type %T", key, fn)
	}
	if typeOf.IsVariadic() {
		return Function{}, fmt.Errorf("luban: function %q must not be variadic", key)
	}
	if typeOf.NumIn() == 0 || typeOf.In(0) != ginContextType {
		return Function{}, fmt.Errorf("luban: function %q must take *gin.Context as its first argument", key)
	}
	if typeOf.NumIn() > 2 {
		return Function{}, fmt.Errorf("luban: function %q may take at most one business input", key)
	}
	if typeOf.NumOut() > 2 {
		return Function{}, fmt.Errorf("luban: function %q may return at most one business output and error", key)
	}

	info := Function{Value: value}
	if typeOf.NumIn() == 2 {
		info.HasInput = true
		info.InputType = typeOf.In(1)
	}
	if typeOf.NumOut() == 1 {
		out := typeOf.Out(0)
		if out == errorType {
			info.HasError = true
		} else if out.Implements(errorType) {
			return Function{}, fmt.Errorf("luban: function %q must return errors through the error interface, not %v", key, out)
		} else {
			info.HasOutput = true
			info.OutputType = out
		}
	}
	if typeOf.NumOut() == 2 {
		if typeOf.Out(1) != errorType {
			return Function{}, fmt.Errorf("luban: function %q must use the exact error interface as its final return value", key)
		}
		if typeOf.Out(0).Implements(errorType) {
			return Function{}, fmt.Errorf("luban: function %q cannot use an error-implementing type as its business output", key)
		}
		info.HasOutput = true
		info.OutputType = typeOf.Out(0)
		info.HasError = true
	}
	return info, nil
}

// Invoke runs a function with the supplied Gin Context and its adjacent node
// value. Panics are converted to errors at this executor boundary.
func Invoke(key string, function Function, ctx *gin.Context, input any, hasInput bool) (result Result, err error) {
	args := []reflect.Value{reflect.ValueOf(ctx)}
	if function.HasInput {
		value, inputErr := AdaptInput(input, hasInput, function.InputType)
		if inputErr != nil {
			return Result{}, fmt.Errorf("luban: function %q input: %w", key, inputErr)
		}
		args = append(args, value)
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			result = Result{}
			err = fmt.Errorf("luban: function %q panicked: %v", key, recovered)
		}
	}()

	values := function.Value.Call(args)
	if function.HasError {
		errorValue := values[len(values)-1]
		if !errorValue.IsNil() {
			callbackErr, ok := errorValue.Interface().(error)
			if !ok {
				return Result{}, fmt.Errorf("luban: function %q returned an invalid error value", key)
			}
			err = callbackErr
		}
	}
	if function.HasOutput {
		result = Result{Value: values[0].Interface(), HasValue: true}
	}
	return result, err
}

// AdaptInput applies Go assignability rules without conversions or pointer
// indirection.
func AdaptInput(input any, hasInput bool, target reflect.Type) (reflect.Value, error) {
	if !hasInput {
		return reflect.Value{}, fmt.Errorf("previous node has no business output")
	}
	if input == nil {
		if !isNilable(target) {
			return reflect.Value{}, fmt.Errorf("nil is not assignable to %v", target)
		}
		return reflect.Zero(target), nil
	}
	value := reflect.ValueOf(input)
	if !value.Type().AssignableTo(target) {
		return reflect.Value{}, fmt.Errorf("value type %v is not assignable to %v", value.Type(), target)
	}
	return value, nil
}

func isNilInterface(value any) bool {
	if value == nil {
		return true
	}
	ref := reflect.ValueOf(value)
	switch ref.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return ref.IsNil()
	default:
		return false
	}
}

func isNilable(valueType reflect.Type) bool {
	switch valueType.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return true
	default:
		return false
	}
}
