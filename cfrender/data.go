package cfrender

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/titanous/json5"
)

// DataFlag is embedded by all CF-backed commands to support -d/--data.
type DataFlag struct {
	Data string `short:"d" help:"Request body: inline JSON5, @file, or - for stdin" optional:""`
}

// HasData reports whether -d was provided.
func (f *DataFlag) HasData() bool { return f.Data != "" }

// ParseBody resolves -d into standard JSON bytes.
//   - "-"      → read stdin
//   - "@path"  → read file
//   - other    → treat as inline JSON5
//
// Returns nil if -d was not provided.
func (f *DataFlag) ParseBody() ([]byte, error) {
	if f.Data == "" {
		return nil, nil
	}

	raw, err := f.readRaw()
	if err != nil {
		return nil, err
	}

	var m map[string]any
	if err := json5.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("invalid JSON5 in -d: %w", err)
	}
	return json.Marshal(m)
}

func (f *DataFlag) readRaw() ([]byte, error) {
	switch {
	case f.Data == "-":
		return io.ReadAll(os.Stdin)
	case strings.HasPrefix(f.Data, "@"):
		return os.ReadFile(f.Data[1:])
	default:
		return []byte(f.Data), nil
	}
}

// ResolveValue resolves a value that may be a @file reference.
func ResolveValue(val string) (string, error) {
	if strings.HasPrefix(val, "@") {
		data, err := os.ReadFile(val[1:])
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(data)), nil
	}
	return val, nil
}
