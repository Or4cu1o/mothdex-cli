package update

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

type stateField struct {
	name  string
	value json.RawMessage
}
type stateKnown State

// UnmarshalJSON preserves unknown v1 fields as inert data in their original order.
func (s *State) UnmarshalJSON(b []byte) error {
	var known stateKnown
	if err := json.Unmarshal(b, &known); err != nil {
		return err
	}
	knownJSON, err := json.Marshal(known)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err = json.Unmarshal(knownJSON, &fields); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	if _, err = d.Token(); err != nil {
		return err
	}
	for d.More() {
		token, e := d.Token()
		if e != nil {
			return e
		}
		name, ok := token.(string)
		if !ok {
			return fmt.Errorf("invalid state field")
		}
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			return e
		}
		if _, ok = fields[strings.ToLower(name)]; !ok {
			known.extra = append(known.extra, stateField{name, value})
		}
	}
	*s = State(known)
	return nil
}
func marshalState(s State) ([]byte, error) {
	b, err := json.MarshalIndent(stateKnown(s), "", "  ")
	if err != nil {
		return nil, err
	}
	b = bytes.TrimSuffix(b, []byte("\n}"))
	for _, field := range s.extra {
		name, e := json.Marshal(field.name)
		if e != nil {
			return nil, e
		}
		var compact bytes.Buffer
		if e = json.Compact(&compact, field.value); e != nil {
			return nil, e
		}
		b = append(b, ',', '\n', ' ', ' ')
		b = append(b, name...)
		b = append(b, ':', ' ')
		b = append(b, compact.Bytes()...)
	}
	b = append(b, '\n', '}')
	if len(b)+1 > 16<<10 {
		return nil, fmt.Errorf("state exceeds size limit")
	}
	return b, nil
}
