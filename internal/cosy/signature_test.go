package cosy

import "testing"

func TestSign(t *testing.T) {
	// Vector derived from the Java algorithm: md5("cosy&" + SECRET + "&" + date)
	got := Sign("Fri, 19 Sep 2026 00:00:00 GMT")
	want := "d17070d0361a6538b3ecb472d778209f"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
}

func TestCurrentDate(t *testing.T) {
	d := CurrentDate()
	if len(d) != 29 { // "Fri, 19 Sep 2026 00:00:00 GMT"
		t.Fatalf("unexpected RFC1123 length: %q", d)
	}
	if d[len(d)-3:] != "GMT" {
		t.Fatalf("expected GMT suffix, got %q", d)
	}
}
