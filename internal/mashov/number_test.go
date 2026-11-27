package mashov

import (
	"encoding/json"
	"testing"
)

// Regression: the live account sends some numbers as decimals ("0.0", "-1.0").
// Decoding those into int failed and stopped the first sync.
func TestNumberAcceptsIntegersDecimalsAndQuotes(t *testing.T) {
	for in, want := range map[string]int{`92`: 92, `92.0`: 92, `-1.0`: -1, `"7"`: 7, `null`: 0} {
		var n Number
		if err := json.Unmarshal([]byte(in), &n); err != nil {
			t.Fatalf("%s: %v", in, err)
		}
		if n.Int() != want {
			t.Fatalf("%s = %d, want %d", in, n.Int(), want)
		}
	}
	var bad Number
	if err := json.Unmarshal([]byte(`"x"`), &bad); err == nil {
		t.Fatal("non-number accepted")
	}
}

func TestNumberMarshalsWholeValuesAsIntegers(t *testing.T) {
	b, _ := json.Marshal(Number(92))
	if string(b) != "92" {
		t.Fatalf("marshal = %s", b)
	}
}
