package bridge

import "testing"

func TestFillTemplate(t *testing.T) {
	in := []byte(`{"a":"{UUID1}","b":"{UUID2}","c":"{UUID3}","d":"{UUID4}","e":"{UUID5}","t":{TIME1}}`)
	out, err := FillTemplate(in, []string{"u1", "u2", "u3", "u4", "u5"}, 1234)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":"u1","b":"u2","c":"u3","d":"u4","e":"u5","t":1234}`
	if string(out) != want {
		t.Fatalf("got %s want %s", out, want)
	}
}

func TestEmbeddedTemplate(t *testing.T) {
	b := LoadEmbeddedTemplate()
	if len(b) == 0 {
		t.Fatal("embedded template empty")
	}
}
