package oauth

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"unicode"
)

// Security-critical client metadata must have unambiguous JSON member names.
// Go's decoder otherwise tolerates duplicates and case-insensitive aliases.
func strictJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := uniqueValue(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return json.Unmarshal(data, out)
}

func uniqueValue(decoder *json.Decoder, depth int) error {
	if depth > 32 {
		return errors.New("JSON too deeply nested")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for decoder.More() {
			name, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := name.(string)
			if !ok {
				return errors.New("invalid JSON key")
			}
			var folded []rune
			for _, char := range key {
				min := char
				for next := unicode.SimpleFold(char); next != char; next = unicode.SimpleFold(next) {
					if next < min {
						min = next
					}
				}
				folded = append(folded, min)
			}
			key = string(folded)
			if seen[key] {
				return errors.New("duplicate JSON key")
			}
			seen[key] = true
			if err := uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for decoder.More() {
			if err := uniqueValue(decoder, depth+1); err != nil {
				return err
			}
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	_, err = decoder.Token()
	return err
}
