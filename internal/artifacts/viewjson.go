package artifacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// decodeSingleJSON checks that data is exactly one well-formed JSON value.
func decodeSingleJSON(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("more than one JSON value")
	}
	return nil
}
