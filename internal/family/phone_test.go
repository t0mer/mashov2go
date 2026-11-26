package family

import "testing"

func TestNormalizePhone(t *testing.T) {
	cases := map[string]string{
		"0501234567":       "972501234567",
		"050-123-4567":     "972501234567",
		"+972 50 123 4567": "972501234567",
		"00972501234567":   "972501234567",
		"":                 "",
	}
	for in, want := range cases {
		got, err := NormalizePhone(in)
		if err != nil || got != want {
			t.Errorf("NormalizePhone(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"12", "abc", "05012"} {
		if _, err := NormalizePhone(bad); err == nil {
			t.Errorf("NormalizePhone(%q) should fail", bad)
		}
	}
}
