package cosy

import (
	"crypto/md5"
	"encoding/hex"
	"time"
)

// Values replicated verbatim from Java Signature.java.
const APPCODE = "cosy"
const SEP = "&"
const secret = "d2FyLCB3YXIgbmV2ZXIgY2hhbmdlcw==" // base64("war, war never changes")

// currentDateLayout matches Java's SimpleDateFormat "EEE, dd MMM yyyy HH:mm:ss 'GMT'".
// A literal "GMT" suffix is used (quoted in Java) rather than time.RFC1123's zone
// shorthand, which would render "UTC" for a UTC time.
const currentDateLayout = "Mon, 02 Jan 2006 15:04:05 GMT"

// CurrentDate formats now as RFC-1123 GMT ("EEE, dd MMM yyyy HH:mm:ss 'GMT'").
func CurrentDate() string {
	return time.Now().UTC().Format(currentDateLayout)
}

// Sign produces the lowercase-hex MD5 of appcode&secret&date.
func Sign(date string) string {
	h := md5.Sum([]byte(APPCODE + SEP + secret + SEP + date))
	return hex.EncodeToString(h[:])
}

// md5Hex kept for parity naming and reuse by BearerBuilder below.
func md5Hex(s string) string {
	h := md5.Sum([]byte(s))
	return hex.EncodeToString(h[:])
}
