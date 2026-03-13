package pydict

import (
	"testing"
)

func TestParseSimpleDict(t *testing.T) {
	input := `{"name": "alice", "age": 30}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["name"] != "alice" {
		t.Errorf("name = %v, want alice", m["name"])
	}
	if m["age"] != int64(30) {
		t.Errorf("age = %v (%T), want 30", m["age"], m["age"])
	}
}

func TestParseSingleQuotes(t *testing.T) {
	input := `{'key': 'value'}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["key"] != "value" {
		t.Errorf("key = %v, want value", m["key"])
	}
}

func TestParseTripleQuotes(t *testing.T) {
	input := `{"text": """hello
world
line 3"""}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := "hello\nworld\nline 3"
	if m["text"] != want {
		t.Errorf("text = %q, want %q", m["text"], want)
	}
}

func TestParseTripleSingleQuotes(t *testing.T) {
	input := `{"text": '''contains "double" quotes'''}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := `contains "double" quotes`
	if m["text"] != want {
		t.Errorf("text = %q, want %q", m["text"], want)
	}
}

func TestParseTrailingCommas(t *testing.T) {
	input := `{"a": 1, "b": 2,}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["a"] != int64(1) || m["b"] != int64(2) {
		t.Errorf("unexpected values: %v", m)
	}
}

func TestParseBoolsAndNone(t *testing.T) {
	input := `{"a": True, "b": False, "c": None}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["a"] != true {
		t.Errorf("a = %v, want true", m["a"])
	}
	if m["b"] != false {
		t.Errorf("b = %v, want false", m["b"])
	}
	if m["c"] != nil {
		t.Errorf("c = %v, want nil", m["c"])
	}
}

func TestParseNestedDict(t *testing.T) {
	input := `{"outer": {"inner": "value"}}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	outer, ok := m["outer"].(map[string]any)
	if !ok {
		t.Fatalf("outer is %T, want map", m["outer"])
	}
	if outer["inner"] != "value" {
		t.Errorf("inner = %v, want value", outer["inner"])
	}
}

func TestParseList(t *testing.T) {
	input := `{"items": [1, "two", True, None,]}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("items is %T, want []any", m["items"])
	}
	if len(items) != 4 {
		t.Fatalf("len = %d, want 4", len(items))
	}
	if items[0] != int64(1) {
		t.Errorf("[0] = %v", items[0])
	}
	if items[1] != "two" {
		t.Errorf("[1] = %v", items[1])
	}
	if items[2] != true {
		t.Errorf("[2] = %v", items[2])
	}
	if items[3] != nil {
		t.Errorf("[3] = %v", items[3])
	}
}

func TestParseFloat(t *testing.T) {
	input := `{"pi": 3.14, "neg": -1.5}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["pi"] != 3.14 {
		t.Errorf("pi = %v", m["pi"])
	}
	if m["neg"] != -1.5 {
		t.Errorf("neg = %v", m["neg"])
	}
}

func TestRoundTrip(t *testing.T) {
	original := map[string]any{
		"name":    "test",
		"count":   int64(42),
		"enabled": true,
		"message": "hello\nworld\nline 3",
		"tags":    []any{"a", "b"},
	}
	encoded := Encode(original)
	decoded, err := Parse(encoded)
	if err != nil {
		t.Fatalf("parse failed: %v\nencoded:\n%s", err, encoded)
	}
	if decoded["name"] != "test" {
		t.Errorf("name = %v", decoded["name"])
	}
	if decoded["count"] != int64(42) {
		t.Errorf("count = %v (%T)", decoded["count"], decoded["count"])
	}
	if decoded["enabled"] != true {
		t.Errorf("enabled = %v", decoded["enabled"])
	}
	if decoded["message"] != "hello\nworld\nline 3" {
		t.Errorf("message = %q", decoded["message"])
	}
}

func TestEncodeTripleQuoteEscaping(t *testing.T) {
	// String containing """ should use ''' instead
	s := `contains """ inside`
	m := map[string]any{"text": s}
	encoded := Encode(m)
	decoded, err := Parse(encoded)
	if err != nil {
		t.Fatalf("parse failed: %v\nencoded:\n%s", err, encoded)
	}
	if decoded["text"] != s {
		t.Errorf("text = %q, want %q", decoded["text"], s)
	}
}

func TestParseEmptyDict(t *testing.T) {
	m, err := Parse("{}")
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 0 {
		t.Errorf("expected empty dict, got %v", m)
	}
}

func TestParseComment(t *testing.T) {
	input := `{
		# this is a comment
		"key": "value",  # inline comment
	}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["key"] != "value" {
		t.Errorf("key = %v", m["key"])
	}
}

// --- Additional edge cases ---

func TestParseLowercaseKeywords(t *testing.T) {
	input := `{"a": true, "b": false, "c": null, "d": nil}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["a"] != true {
		t.Errorf("a = %v", m["a"])
	}
	if m["b"] != false {
		t.Errorf("b = %v", m["b"])
	}
	if m["c"] != nil {
		t.Errorf("c = %v", m["c"])
	}
	if m["d"] != nil {
		t.Errorf("d = %v", m["d"])
	}
}

func TestParseNegativeInt(t *testing.T) {
	input := `{"val": -42}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["val"] != int64(-42) {
		t.Errorf("val = %v (%T)", m["val"], m["val"])
	}
}

func TestParseScientificNotation(t *testing.T) {
	input := `{"val": 1e10, "neg": -2.5E-3}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	if m["val"] != 1e10 {
		t.Errorf("val = %v", m["val"])
	}
	if m["neg"] != -2.5e-3 {
		t.Errorf("neg = %v", m["neg"])
	}
}

func TestParseEscapedStrings(t *testing.T) {
	input := `{"text": "hello\nworld\ttab\\slash\"quote"}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	want := "hello\nworld\ttab\\slash\"quote"
	if m["text"] != want {
		t.Errorf("text = %q, want %q", m["text"], want)
	}
}

func TestParseEmptyList(t *testing.T) {
	input := `{"items": []}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	items, ok := m["items"].([]any)
	if !ok {
		t.Fatalf("items is %T", m["items"])
	}
	if len(items) != 0 {
		t.Errorf("expected empty list, got %v", items)
	}
}

func TestParseNestedList(t *testing.T) {
	input := `{"matrix": [[1, 2], [3, 4]]}`
	m, err := Parse(input)
	if err != nil {
		t.Fatal(err)
	}
	matrix, ok := m["matrix"].([]any)
	if !ok {
		t.Fatalf("matrix is %T", m["matrix"])
	}
	if len(matrix) != 2 {
		t.Fatalf("len = %d", len(matrix))
	}
	row0, ok := matrix[0].([]any)
	if !ok {
		t.Fatalf("row0 is %T", matrix[0])
	}
	if row0[0] != int64(1) || row0[1] != int64(2) {
		t.Errorf("row0 = %v", row0)
	}
}

func TestParseError_UnterminatedDict(t *testing.T) {
	_, err := Parse("{\"key\": \"value\"")
	if err == nil {
		t.Fatal("expected error for unterminated dict")
	}
}

func TestParseError_UnterminatedString(t *testing.T) {
	_, err := Parse(`{"key": "unterminated}`)
	if err == nil {
		t.Fatal("expected error for unterminated string")
	}
}

func TestParseError_TopLevelNotDict(t *testing.T) {
	_, err := Parse(`[1, 2, 3]`)
	if err == nil {
		t.Fatal("expected error for non-dict top level")
	}
}

func TestEncodeOrdered(t *testing.T) {
	pairs := []KV{
		{"z_key", "first"},
		{"a_key", "second"},
	}
	encoded := EncodeOrdered(pairs)
	decoded, err := Parse(encoded)
	if err != nil {
		t.Fatalf("parse failed: %v\nencoded:\n%s", err, encoded)
	}
	if decoded["z_key"] != "first" {
		t.Errorf("z_key = %v", decoded["z_key"])
	}
	if decoded["a_key"] != "second" {
		t.Errorf("a_key = %v", decoded["a_key"])
	}
}

func TestEncodeOrderedEmpty(t *testing.T) {
	encoded := EncodeOrdered(nil)
	if encoded != "{}" {
		t.Errorf("got %q, want {}", encoded)
	}
}

func TestEncodeNilValue(t *testing.T) {
	m := map[string]any{"key": nil}
	encoded := Encode(m)
	if decoded, err := Parse(encoded); err != nil {
		t.Fatal(err)
	} else if decoded["key"] != nil {
		t.Errorf("expected nil, got %v", decoded["key"])
	}
}

func TestEncodeBoolValues(t *testing.T) {
	m := map[string]any{"t": true, "f": false}
	encoded := Encode(m)
	decoded, err := Parse(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if decoded["t"] != true {
		t.Errorf("t = %v", decoded["t"])
	}
	if decoded["f"] != false {
		t.Errorf("f = %v", decoded["f"])
	}
}

func TestEncodeEmptyMap(t *testing.T) {
	encoded := Encode(map[string]any{})
	if encoded != "{}" {
		t.Errorf("got %q, want {}", encoded)
	}
}

func TestEncodeStringWithQuotes(t *testing.T) {
	m := map[string]any{"text": `he said "hello"`}
	encoded := Encode(m)
	decoded, err := Parse(encoded)
	if err != nil {
		t.Fatalf("parse failed: %v\nencoded:\n%s", err, encoded)
	}
	if decoded["text"] != `he said "hello"` {
		t.Errorf("text = %q", decoded["text"])
	}
}
