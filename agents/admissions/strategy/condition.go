package strategyagent

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
)

// EvaluateCondition evaluates literal-only conditions. A string value is
// always a string, never an implicit reference to another student's field.
func EvaluateCondition(condition Condition, fields map[string]Literal) (bool, error) {
	return evaluateCondition(condition, fields, 0)
}

func evaluateCondition(c Condition, fields map[string]Literal, depth int) (bool, error) {
	if depth > 32 {
		return false, fmt.Errorf("condition exceeds maximum depth")
	}
	switch c.Op {
	case "and", "or":
		if len(c.Children) == 0 || c.Child != nil || c.Field != "" || len(c.Value) > 0 {
			return false, fmt.Errorf("invalid %s condition", c.Op)
		}
		result := c.Op == "and"
		for _, child := range c.Children {
			value, err := evaluateCondition(child, fields, depth+1)
			if err != nil {
				return false, err
			}
			if c.Op == "and" {
				result = result && value
			} else {
				result = result || value
			}
		}
		return result, nil
	case "not":
		if c.Child == nil || len(c.Children) > 0 || c.Field != "" || len(c.Value) > 0 {
			return false, fmt.Errorf("invalid not condition")
		}
		value, err := evaluateCondition(*c.Child, fields, depth+1)
		return !value, err
	}
	if c.Field == "" || len(c.Children) > 0 || c.Child != nil {
		return false, fmt.Errorf("invalid leaf condition")
	}
	raw, exists := fields[c.Field]
	if !exists {
		return false, fmt.Errorf("condition field %q is missing", c.Field)
	}
	var actual, expected any
	if json.Unmarshal(raw, &actual) != nil || json.Unmarshal(c.Value, &expected) != nil {
		return false, fmt.Errorf("condition requires valid JSON literals")
	}
	if !literalValue(expected) {
		return false, fmt.Errorf("condition value must be a scalar or scalar array")
	}
	switch c.Op {
	case "eq":
		if _, array := expected.([]any); array {
			return false, fmt.Errorf("eq requires a scalar")
		}
		return reflect.DeepEqual(actual, expected), nil
	case "neq":
		if _, array := expected.([]any); array {
			return false, fmt.Errorf("neq requires a scalar")
		}
		return !reflect.DeepEqual(actual, expected), nil
	case "in", "nin":
		values, ok := expected.([]any)
		if !ok {
			return false, fmt.Errorf("%s requires an array", c.Op)
		}
		found := false
		actuals, isArray := actual.([]any)
		if !isArray {
			actuals = []any{actual}
		}
		for _, value := range values {
			for _, item := range actuals {
				if reflect.DeepEqual(item, value) {
					found = true
				}
			}
		}
		if c.Op == "nin" {
			found = !found
		}
		return found, nil
	case "gte", "lte", "between":
		number, ok := actual.(float64)
		if !ok {
			return false, fmt.Errorf("%s requires a numeric field", c.Op)
		}
		if c.Op == "between" {
			values, ok := expected.([]any)
			if !ok || len(values) != 2 {
				return false, fmt.Errorf("between requires two numeric bounds")
			}
			min, first := values[0].(float64)
			max, second := values[1].(float64)
			if !first || !second || min > max {
				return false, fmt.Errorf("between requires ordered numeric bounds")
			}
			return number >= min && number <= max, nil
		}
		bound, ok := expected.(float64)
		if !ok {
			return false, fmt.Errorf("numeric comparison requires a numeric literal; field references are not supported")
		}
		if c.Op == "gte" {
			return number >= bound, nil
		}
		return number <= bound, nil
	default:
		return false, fmt.Errorf("unsupported condition operator %q", c.Op)
	}
}

func literalValue(value any) bool {
	switch v := value.(type) {
	case nil, string, bool:
		return true
	case float64:
		return !math.IsInf(v, 0) && !math.IsNaN(v)
	case []any:
		for _, item := range v {
			if _, nested := item.([]any); nested || !literalValue(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}
