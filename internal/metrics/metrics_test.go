package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestCounterExposition(t *testing.T) {
	r := NewRegistry()
	d := r.Counter("mashov2go_deliveries_total", "Deliveries by channel and status.", "channel", "status")
	d.Inc("whatsapp", "sent")
	d.Inc("whatsapp", "sent")
	d.Inc("telegram", "failed")
	if d.Value("whatsapp", "sent") != 2 {
		t.Fatalf("value = %v", d.Value("whatsapp", "sent"))
	}

	var b bytes.Buffer
	if err := r.Write(&b); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"# TYPE mashov2go_deliveries_total counter",
		`mashov2go_deliveries_total{channel="whatsapp",status="sent"} 2`,
		`mashov2go_deliveries_total{channel="telegram",status="failed"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestLabelValuesAreEscaped(t *testing.T) {
	r := NewRegistry()
	c := r.Counter("x_total", "x", "v")
	c.Inc("a\"b\\c\nd")
	var b bytes.Buffer
	_ = r.Write(&b)
	if !strings.Contains(b.String(), `x_total{v="a\"b\\c\nd"} 1`) {
		t.Fatalf("not escaped:\n%s", b.String())
	}
}

func TestNilCounterAndRegistryAreSafe(t *testing.T) {
	var c *Counter
	c.Inc("ignored")
	var r *Registry
	if err := r.Write(&bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
}

func TestWrongLabelCountPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected a panic for the wrong label count")
		}
	}()
	NewRegistry().Counter("y_total", "y", "a", "b").Inc("only-one")
}
