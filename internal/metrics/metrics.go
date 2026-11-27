// Package metrics is a small Prometheus-text registry: labelled counters and
// their exposition (CLAUDE.md §12). It is dependency-free and nil-safe, so
// callers can run without a registry.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// Registry holds the counters of one process.
type Registry struct {
	mu       sync.Mutex
	counters map[string]*Counter
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{counters: map[string]*Counter{}}
}

// Counter returns the counter with this name, creating it on first use. labels
// are the label names; every Inc must pass one value per label.
func (r *Registry) Counter(name, help string, labels ...string) *Counter {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.counters[name]; ok {
		return c
	}
	c := &Counter{name: name, help: help, labels: labels, values: map[string]float64{}}
	r.counters[name] = c
	return c
}

// Write writes every counter in Prometheus text exposition format, sorted by name.
func (r *Registry) Write(w io.Writer) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	names := make([]string, 0, len(r.counters))
	for n := range r.counters {
		names = append(names, n)
	}
	r.mu.Unlock()
	sort.Strings(names)
	for _, n := range names {
		r.mu.Lock()
		c := r.counters[n]
		r.mu.Unlock()
		if err := c.write(w); err != nil {
			return err
		}
	}
	return nil
}

// Counter is a monotonically increasing value per label set.
type Counter struct {
	name   string
	help   string
	labels []string

	mu     sync.Mutex
	values map[string]float64
}

// Inc adds one for the given label values. It is a no-op on a nil Counter.
func (c *Counter) Inc(labelValues ...string) {
	if c == nil {
		return
	}
	if len(labelValues) != len(c.labels) {
		panic(fmt.Sprintf("metrics: %s takes %d label values, got %d", c.name, len(c.labels), len(labelValues)))
	}
	key := strings.Join(labelValues, "\x00")
	c.mu.Lock()
	c.values[key]++
	c.mu.Unlock()
}

// Value returns the current count for the given label values (tests and diagnostics).
func (c *Counter) Value(labelValues ...string) float64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.values[strings.Join(labelValues, "\x00")]
}

func (c *Counter) write(w io.Writer) error {
	c.mu.Lock()
	keys := make([]string, 0, len(c.values))
	for k := range c.values {
		keys = append(keys, k)
	}
	vals := make(map[string]float64, len(c.values))
	for k, v := range c.values {
		vals[k] = v
	}
	c.mu.Unlock()
	sort.Strings(keys)

	if _, err := fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name); err != nil {
		return err
	}
	if len(c.labels) == 0 {
		v := vals[""]
		_, err := fmt.Fprintf(w, "%s %g\n", c.name, v)
		return err
	}
	for _, k := range keys {
		parts := strings.Split(k, "\x00")
		pairs := make([]string, 0, len(c.labels))
		for i, l := range c.labels {
			pairs = append(pairs, fmt.Sprintf(`%s="%s"`, l, escape(parts[i])))
		}
		if _, err := fmt.Fprintf(w, "%s{%s} %g\n", c.name, strings.Join(pairs, ","), vals[k]); err != nil {
			return err
		}
	}
	return nil
}

// escape applies the Prometheus label-value escaping rules.
func escape(v string) string {
	return strings.NewReplacer(`\`, `\\`, "\n", `\n`, `"`, `\"`).Replace(v)
}
