package bridge

import (
	_ "embed"
	"strconv"
	"strings"
)

//go:embed baseprompt.json
var embeddedTemplate []byte

// LoadEmbeddedTemplate returns the verbatim baseprompt.json bytes.
func LoadEmbeddedTemplate() []byte {
	return embeddedTemplate
}

// FillTemplate substitutes {UUID1}..{UUID5} (in order) and {TIME1} (milliseconds)
// exactly as the original Java bridge did before parsing with Jackson, matching
// the wire contract the Qoder template expects.
func FillTemplate(template []byte, uuids []string, nowMillis int64) ([]byte, error) {
	s := string(template)
	for i := 0; i < 5 && i < len(uuids); i++ {
		s = strings.ReplaceAll(s, "{UUID"+strconv.Itoa(i+1)+"}", uuids[i])
	}
	s = strings.ReplaceAll(s, "{TIME1}", strconv.FormatInt(nowMillis, 10))
	return []byte(s), nil
}
