package engine

import "testing"

func TestPropertyValuesEqualPercentageNumerically(t *testing.T) {
	tests := []struct {
		name       string
		left       interface{}
		right      interface{}
		wantEqual  bool
	}{
		{name: "integer text equals decimal text", left: "10", right: "10.0", wantEqual: true},
		{name: "numeric equals text", left: 10, right: "10.00", wantEqual: true},
		{name: "fractional forms compare numerically", left: "6.50", right: "6.5", wantEqual: true},
		{name: "different percentage remains different", left: "9", right: "10.0", wantEqual: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := propertyValuesEqual("percentage", tt.left, tt.right); got != tt.wantEqual {
				t.Fatalf("propertyValuesEqual(percentage, %v, %v) = %v, want %v", tt.left, tt.right, got, tt.wantEqual)
			}
		})
	}
}

func TestPropertyValuesEqualDoesNotNormalizeArbitraryStrings(t *testing.T) {
	if propertyValuesEqual("name", "10", "10.0") {
		t.Fatal("non-percentage string values must keep ordinary comparison semantics")
	}
}
