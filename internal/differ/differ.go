// Package differ turns a fresh Mashov fetch into new, updated and removed items
// (CLAUDE.md §5.3). It is pure: callers handle storage and events.
package differ

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/t0mer/mashov2go/internal/store"
)

// Current is one record from the latest fetch.
type Current struct {
	Key     string
	Hash    string
	Payload string // canonical JSON
}

// Change is a new or updated record that should produce an event.
type Change struct {
	Kind    string // "new" | "updated"
	Current Current
}

// Result is the classification of one fetch against stored state.
type Result struct {
	Changes []Change // empty during baseline
	Removed []string // keys present before and absent now (soft-deleted, no event)
}

// Normalize turns raw response items into Currents for a domain, using the
// per-domain key rules from CLAUDE.md §5.3. Duplicate keys are an error because
// they would make the diff ambiguous.
func Normalize(domain string, raw []json.RawMessage) ([]Current, error) {
	out := make([]Current, 0, len(raw))
	seen := map[string]bool{}
	for _, r := range raw {
		var obj map[string]any
		if err := json.Unmarshal(r, &obj); err != nil {
			return nil, fmt.Errorf("%s item is not an object: %w", domain, err)
		}
		canon, err := canonical(obj)
		if err != nil {
			return nil, err
		}
		key, err := KeyFor(domain, obj)
		if err != nil {
			return nil, err
		}
		if seen[key] {
			return nil, fmt.Errorf("%s: duplicate item key %q", domain, key)
		}
		seen[key] = true
		out = append(out, Current{Key: key, Hash: hashOf(canon), Payload: string(canon)})
	}
	return out, nil
}

// KeyFor returns the stable identity of an item within its domain (CLAUDE.md §5.3).
func KeyFor(domain string, obj map[string]any) (string, error) {
	switch domain {
	case "homework":
		if id, ok := num(obj["lessonId"]); ok && id != "" {
			return "lesson:" + id, nil
		}
		// Fallback when lessonId is absent: subject + date + text.
		h := sha256.Sum256([]byte(str(obj["subjectName"]) + "|" + str(obj["lessonDate"]) + "|" + str(obj["homework"])))
		return "hash:" + hex.EncodeToString(h[:8]), nil
	case "grades":
		return need(domain, obj, "gradingEventId")
	case "behave":
		return joined(domain, obj, "lessonId", "eventCode", "lessonDate")
	case "maakav":
		return need(domain, obj, "maakavId")
	case "timetable-changes":
		return joined(domain, obj, "groupId", "lessonDate", "lesson", "action")
	case "messageboard":
		return need(domain, obj, "eventid")
	case "justifications":
		return need(domain, obj, "justificationId")
	}
	return "", fmt.Errorf("no key rule for domain %q", domain)
}

// Classify compares the latest fetch with stored items. When baseline is true the
// caller is doing the first silent sync of a (child, domain): nothing is reported.
func Classify(prev []store.Item, cur []Current, baseline bool) Result {
	prevByKey := make(map[string]store.Item, len(prev))
	for _, p := range prev {
		prevByKey[p.Key] = p
	}
	curKeys := make(map[string]bool, len(cur))
	var res Result
	for _, c := range cur {
		curKeys[c.Key] = true
		if baseline {
			continue
		}
		p, existed := prevByKey[c.Key]
		switch {
		case !existed:
			res.Changes = append(res.Changes, Change{Kind: "new", Current: c})
		case p.ContentHash != c.Hash:
			res.Changes = append(res.Changes, Change{Kind: "updated", Current: c})
		}
	}
	for _, p := range prev {
		if !curKeys[p.Key] {
			res.Removed = append(res.Removed, p.Key)
		}
	}
	return res
}

func canonical(obj map[string]any) ([]byte, error) {
	// encoding/json sorts map keys, so the output is stable for hashing.
	b, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, b); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func hashOf(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func need(domain string, obj map[string]any, field string) (string, error) {
	v, ok := num(obj[field])
	if !ok || v == "" {
		return "", fmt.Errorf("%s item missing %s", domain, field)
	}
	return v, nil
}

func joined(domain string, obj map[string]any, fields ...string) (string, error) {
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		v, ok := num(obj[f])
		if !ok {
			return "", fmt.Errorf("%s item missing %s", domain, f)
		}
		parts = append(parts, v)
	}
	return strings.Join(parts, "|"), nil
}

// num renders a JSON scalar as text. Ids may arrive as numbers or strings.
func num(v any) (string, bool) {
	switch t := v.(type) {
	case float64:
		return fmt.Sprintf("%d", int64(t)), true
	case string:
		return t, true
	}
	return "", false
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
