package cosy

import "testing"

func TestEncodeVectors(t *testing.T) {
	cases := []struct{ plain, want string }{
		{"hello world", "YuHp$Hq&J(WPHFru"},
		{"abc", "MSK#"},
		{"hi", "$HzP"},
		{"", ""},
	}
	for i, c := range cases {
		got := Encode([]byte(c.plain))
		if got != c.want {
			t.Errorf("case %d: encode(%q) = %q, want %q", i, c.plain, got, c.want)
		}
	}
}

func TestRoundTrip(t *testing.T) {
	inputs := [][]byte{
		[]byte("hello world"), []byte("abc"), []byte("hi"), []byte{},
		[]byte("你好世界🍀"),
		bytesRange(),
	}
	for _, in := range inputs {
		enc := Encode(in)
		out, err := Decode(enc)
		if err != nil {
			t.Fatalf("decode(%q) error: %v", enc, err)
		}
		if string(out) != string(in) {
			t.Fatalf("roundtrip failed: got %q want %q", out, in)
		}
	}
}

func bytesRange() []byte {
	b := make([]byte, 256)
	for i := range b {
		b[i] = byte(i)
	}
	return b
}

func TestDecodeOutOfAlphabet(t *testing.T) {
	// ':' is not in the custom alphabet.
	if _, err := Decode(":"); err == nil {
		t.Fatal("expected error for out-of-alphabet char")
	}
}
