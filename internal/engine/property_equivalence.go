package engine

import (
	"fmt"
	"math/big"
	"reflect"
	"strings"
)

// propertyValuesEqual compares provider/config values using the semantics
// of the property rather than only their textual representation.
func propertyValuesEqual(path string, left, right interface{}) bool {
	if path == "percentage" {
		if l, ok := percentageNumber(left); ok {
			if r, ok := percentageNumber(right); ok {
				return l.Cmp(r) == 0
			}
		}
	}

	return reflect.DeepEqual(canonical(left), canonical(right))
}

func percentageNumber(value interface{}) (*big.Rat, bool) {
	text := strings.TrimSpace(fmt.Sprint(value))
	if text == "" {
		return nil, false
	}

	number, ok := new(big.Rat).SetString(text)
	return number, ok
}
