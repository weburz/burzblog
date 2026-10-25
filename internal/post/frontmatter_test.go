package post

import (
	"strings"
	"testing"
)

func TestDecodeAcceptsCoreTags(t *testing.T) {
	cases := map[string]string{
		"plain scalars":  "a: text\nb: 1\nc: 1.5\nd: true\ne: ~\nf: 2026-01-01\n",
		"explicit str":   "a: !!str 123\n",
		"explicit int":   "a: !!int 5\n",
		"explicit float": "a: !!float 5\n",
		"explicit bool":  "a: !!bool true\n",
		"explicit null":  "a: !!null\n",
		"explicit ts":    "a: !!timestamp 2026-01-01\n",
		"explicit map":   "a: !!map {b: 1}\n",
		"explicit seq":   "a: !!seq [1, 2]\n",
		"explicit bin":   "a: !!binary aGk=\n",
		"nested":         "a:\n  b: [1, {c: !!str 2}, [3]]\n  d:\n    e: x\n",
		"merge as value": "a: \"<<\"\n",
	}
	for name, extra := range cases {
		_, err := Decode("---\ntitle: x\n" + extra + "---\n")
		if err != nil {
			t.Errorf("%s: Decode = %v, want success", name, err)
		}
	}
}

func TestDecodeRefusesUnsafeTagsAndMergeKeys(t *testing.T) {
	cases := map[string]struct{ doc, want string }{
		"python tag": {"title: x\nevil: !!python/object/apply:os.system [\"id\"]\n", "!!python/object/apply:os.system"},
		"python map": {"title: x\nevil: !!python/object {a: 1}\n", "!!python/object"},
		"custom tag": {"title: x\na: !foo 1\n", "!foo"},
		"set tag":    {"title: x\na: !!set {b}\n", "!!set"},
		"nested tag": {"title: x\na:\n  b:\n    - !!python/name:os.system\n", "line 4"},
		"tagged key": {"title: x\n!foo a: 1\n", "!foo"},
		"merge top":  {"title: x\n<<: {description: d, slug: ../../etc/x}\n", "merge key"},
		"merge tag":  {"title: x\n!!merge <<: {a: 1}\n", "merge key"},
		"merge val":  {"title: x\na: !!merge y\n", "merge key"},
		"merge nest": {"title: x\na:\n  <<: {b: 1}\n", "line 3"},
		"merge seq":  {"title: x\na:\n  - {<<: {b: 1}}\n", "merge key"},
		"merge quot": {"title: x\n\"<<\": 1\n", "merge key"},
	}
	for name, c := range cases {
		_, err := Decode("---\n" + c.doc + "---\n")
		if err == nil || !IsFieldError(err) {
			t.Errorf("%s: Decode = %v, want a FieldError", name, err)
			continue
		}
		if !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %q does not mention %q", name, err, c.want)
		}
	}
}

func TestReservedKeysStillRefusedInExtra(t *testing.T) {
	for _, key := range []string{"title", "description", "status", "timestamps", "coverImage", "slug"} {
		if _, err := parseExtra(key + ": x\n"); err == nil || !IsFieldError(err) {
			t.Errorf("parseExtra(%s) = %v, want a FieldError", key, err)
		}
	}
}

func TestValidateRefusesUnsafeExtra(t *testing.T) {
	for _, extra := range []string{"a: !!python/object {}\n", "<<: {a: 1}\n", "a: !foo x\n"} {
		p := &Post{Slug: "x", Title: "x", Status: Draft, Extra: extra}
		if err := Validate(p); err == nil {
			t.Errorf("Validate(extra %q) = nil, want an error", extra)
		}
	}
}

func TestExtraRoundTripsUnchanged(t *testing.T) {
	extra := "a: !!str 123\nb:\n  - 1\n  - c: 2026-01-01\nd: !!binary aGk=\ne: \"<<\"\nf:\n  g: ~\n"
	d, err := Decode("---\ntitle: x\n" + extra + "---\n")
	if err != nil {
		t.Fatalf("Decode = %v", err)
	}
	p := &Post{Slug: "x", Status: Draft}
	d.Apply(p)
	doc := Encode(p)
	again, err := Decode(doc)
	if err != nil {
		t.Fatalf("Decode(Encode(p)) = %v\n%s", err, doc)
	}
	if again.Extra != d.Extra {
		t.Errorf("extra changed on round trip:\n got %q\nwant %q", again.Extra, d.Extra)
	}
	for _, want := range []string{"a: !!str 123", "d: !!binary aGk=", "g: ~"} {
		if !strings.Contains(doc, want) {
			t.Errorf("Encode output missing %q:\n%s", want, doc)
		}
	}
}

func TestEncodeOmitsUnsafeStoredExtra(t *testing.T) {
	// Posts saved before the tag and merge-key checks may still hold these.
	for _, extra := range []string{
		"evil: !!python/object/apply:os.system [\"id\"]\n",
		"<<: {slug: ../../etc/x}\n",
		"ok: 1\n<<: {slug: ../../etc/x}\n",
	} {
		p := &Post{Slug: "x", Title: "x", Status: Published, Extra: extra}
		doc := Encode(p)
		for _, bad := range []string{"!!python", "<<", "slug", "etc/x", "ok:"} {
			if strings.Contains(doc, bad) {
				t.Errorf("Encode(extra %q) emitted %q:\n%s", extra, bad, doc)
			}
		}
		if _, err := Decode(doc); err != nil {
			t.Errorf("Decode(Encode(p)) = %v\n%s", err, doc)
		}
	}
}
