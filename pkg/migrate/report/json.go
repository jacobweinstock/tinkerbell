package report

import (
	"encoding/json"
	"io"
)

// WriteJSON writes r as indented JSON to w with a trailing newline.
func WriteJSON(w io.Writer, r Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}
