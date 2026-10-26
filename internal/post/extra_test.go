package post

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestExtraValues(t *testing.T) {
	src := `legal_references:
  - act: Companies Act, 2013
    sections: [2, 134]
    since: 2013-08-29
  - act: IT Act
    sections: []
published_at: 2026-01-02T03:04:05Z
ratio: 1.5
pinned: true
note: null
nested:
  deeper:
    ok: yes please
1: one
true: yes
`
	got, err := ExtraValues(src)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"legal_references": []any{
			map[string]any{"act": "Companies Act, 2013", "sections": []any{2, 134}, "since": "2013-08-29"},
			map[string]any{"act": "IT Act", "sections": []any{}},
		},
		"published_at": "2026-01-02T03:04:05Z",
		"ratio":        1.5,
		"pinned":       true,
		"note":         nil,
		"nested":       map[string]any{"deeper": map[string]any{"ok": "yes please"}},
		"1":            "one",
		"true":         "yes",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtraValues = %#v, want %#v", got, want)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Errorf("result does not encode as JSON: %v", err)
	}
}

func TestExtraValuesNonStringNestedKeysAndInfinity(t *testing.T) {
	got, err := ExtraValues("m:\n  1: a\n  2: b\nmax: .inf\nnone: .nan\n")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["m"], map[string]any{"1": "a", "2": "b"}) || got["max"] != "+Inf" || got["none"] != "NaN" {
		t.Errorf("ExtraValues = %#v", got)
	}
	if _, err := json.Marshal(got); err != nil {
		t.Errorf("result does not encode as JSON: %v", err)
	}
}

func TestExtraValuesEmpty(t *testing.T) {
	for _, src := range []string{"", "  \n\n", "# only a comment\n"} {
		got, err := ExtraValues(src)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("ExtraValues(%q) = %v, %v; want an empty map", src, got, err)
		}
	}
}

func TestExtraValuesRejects(t *testing.T) {
	for name, src := range map[string]string{
		"not a mapping": "- a\n- b\n",
		"broken yaml":   "a: [1, 2\n",
		"modelled key":  "title: nope\n",
		"alias":         "a: &x 1\nb: *x\n",
	} {
		if got, err := ExtraValues(src); err == nil {
			t.Errorf("%s: ExtraValues = %v, want an error", name, got)
		}
	}
}
