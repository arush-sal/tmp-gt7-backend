package rkb

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestRoundTrip_AllTracks asserts every assembled record is JSON-value-equal to its source
// <slug>.rkb.json. Requires the generated artifact (tools/rkb/out); skipped otherwise.
func TestRoundTrip_AllTracks(t *testing.T) {
	base := filepath.Join("..", "..", "tools", "rkb", "out")
	dbPath := filepath.Join(base, "rkb.db")
	if _, err := os.Stat(dbPath); err != nil {
		t.Skip("generated rkb.db not present; run `cd tools/rkb && ./.venv/bin/python -m rkb`")
	}
	st, err := Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = st.Close() }()

	idx, err := st.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 121 {
		t.Fatalf("index len = %d, want 121", len(idx))
	}
	for _, e := range idx {
		rec, found, err := st.Track(context.Background(), e.Slug)
		if err != nil || !found {
			t.Fatalf("%s: found=%v err=%v", e.Slug, found, err)
		}
		got := normalize(t, mustMarshal(t, rec))
		want := normalize(t, mustRead(t, filepath.Join(base, "rkb", e.Slug+".rkb.json")))
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: assembled record != source JSON: %s", e.Slug, diffPath(got, want, "$"))
		}
	}
}

func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// normalize parses JSON to a generic value so comparison ignores key order & formatting, then
// applies the documented canonicalization deviation: a sector label_world of [null,null] (or with any
// null coord) is treated as null (see Global Constraints fidelity rule 3). Both the assembled record
// and the source JSON pass through this, so the Go side emits null and the source side is folded to
// null before comparison.
func normalize(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	canonicalizeLabelWorld(v)
	return v
}

// canonicalizeLabelWorld walks the record and sets sectors[].label_world to nil when it is an array
// containing any null element (degenerate label position == no position).
func canonicalizeLabelWorld(v any) {
	m, ok := v.(map[string]any)
	if !ok {
		return
	}
	secs, ok := m["sectors"].([]any)
	if !ok {
		return
	}
	for _, s := range secs {
		sm, ok := s.(map[string]any)
		if !ok {
			continue
		}
		if lw, ok := sm["label_world"].([]any); ok {
			for _, c := range lw {
				if c == nil {
					sm["label_world"] = nil
					break
				}
			}
		}
	}
}

// diffPath returns a short description of the first structural difference between got and want,
// prefixed with its JSON path. Debug aid for the round-trip gate.
func diffPath(got, want any, path string) string {
	if reflect.DeepEqual(got, want) {
		return ""
	}
	gm, gok := got.(map[string]any)
	wm, wok := want.(map[string]any)
	if gok && wok {
		seen := map[string]bool{}
		for k := range gm {
			seen[k] = true
		}
		for k := range wm {
			seen[k] = true
		}
		for k := range seen {
			gv, gh := gm[k]
			wv, wh := wm[k]
			if gh != wh {
				return path + "." + k + fmt.Sprintf(" presence got=%v want=%v", gh, wh)
			}
			if d := diffPath(gv, wv, path+"."+k); d != "" {
				return d
			}
		}
		return ""
	}
	ga, gok := got.([]any)
	wa, wok := want.([]any)
	if gok && wok {
		if len(ga) != len(wa) {
			return path + fmt.Sprintf(" len got=%d want=%d", len(ga), len(wa))
		}
		for i := range ga {
			if d := diffPath(ga[i], wa[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
				return d
			}
		}
		return ""
	}
	return path + fmt.Sprintf(" got=%v want=%v", got, want)
}
