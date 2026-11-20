package config

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The parsers accept what each layer produces: YAML-native values (file), strings (env,
// flags, set) and the canonical Go types (defaults).

func asString(raw any) (string, error) {
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("expected a string, got %T", raw)
	}
	if strings.ContainsRune(s, 0) {
		return "", errors.New("must not contain NUL")
	}
	return s, nil
}

func asInt(raw any) (int, error) {
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64:
		return intFromInt64(v)
	case uint64:
		if v > math.MaxInt32 {
			return 0, errors.New("number too large")
		}
		return int(v), nil
	case float64:
		if v != math.Trunc(v) || math.Abs(v) > math.MaxInt32 {
			return 0, errors.New("expected a whole number")
		}
		return int(v), nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, errors.New("expected a whole number")
		}
		return intFromInt64(n)
	default:
		return 0, fmt.Errorf("expected a whole number, got %T", raw)
	}
}

func intFromInt64(v int64) (int, error) {
	if v > math.MaxInt32 || v < math.MinInt32 {
		return 0, errors.New("number out of range")
	}
	return int(v), nil
}

func asFloat(raw any) (float64, error) {
	switch v := raw.(type) {
	case float64:
		return checkFinite(v)
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0, errors.New("expected a number")
		}
		return checkFinite(f)
	default:
		n, err := asInt(raw)
		return float64(n), err
	}
}

func checkFinite(f float64) (float64, error) {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, errors.New("expected a finite number")
	}
	return f, nil
}

func asBool(raw any) (bool, error) {
	switch v := raw.(type) {
	case bool:
		return v, nil
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return false, errors.New("expected true or false")
		}
		return b, nil
	default:
		return false, fmt.Errorf("expected true or false, got %T", raw)
	}
}

func asDuration(raw any) (time.Duration, error) {
	switch v := raw.(type) {
	case time.Duration:
		return v, nil
	case string:
		d, err := time.ParseDuration(strings.TrimSpace(v))
		if err != nil {
			return 0, errors.New("expected a Go duration such as 90s, 10m or 168h")
		}
		return d, nil
	default:
		return 0, errors.New("expected a Go duration string such as 90s, 10m or 168h")
	}
}

const (
	kib = int64(1) << 10
	mib = int64(1) << 20
	gib = int64(1) << 30
	tib = int64(1) << 40
)

var sizePattern = regexp.MustCompile(`(?i)^\s*(\d+)\s*(B|KiB|MiB|GiB|TiB)?\s*$`)

var sizeUnits = map[string]int64{"": 1, "B": 1, "KIB": kib, "MIB": mib, "GIB": gib, "TIB": tib}

// asSize parses a byte count: plain digits, or digits with a B, KiB, MiB, GiB or TiB suffix.
func asSize(raw any) (int64, error) {
	switch v := raw.(type) {
	case int64:
		return v, nil
	case int:
		return int64(v), nil
	case uint64:
		if v > math.MaxInt64 {
			return 0, errors.New("size too large")
		}
		return int64(v), nil
	case string:
		return parseSize(v)
	default:
		return 0, errors.New("expected a size such as 512MiB or 256GiB")
	}
}

func parseSize(s string) (int64, error) {
	m := sizePattern.FindStringSubmatch(s)
	if m == nil {
		return 0, errors.New("expected a size such as 512MiB or 256GiB")
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return 0, errors.New("size too large")
	}
	mult := sizeUnits[strings.ToUpper(m[2])]
	if n > math.MaxInt64/mult {
		return 0, errors.New("size too large")
	}
	return n * mult, nil
}

// formatSize renders bytes with the largest unit that divides them exactly.
func formatSize(n int64) string {
	for _, u := range []struct {
		name string
		size int64
	}{{"TiB", tib}, {"GiB", gib}, {"MiB", mib}, {"KiB", kib}} {
		if n >= u.size && n%u.size == 0 {
			return strconv.FormatInt(n/u.size, 10) + u.name
		}
	}
	return strconv.FormatInt(n, 10) + "B"
}

// asList accepts a YAML list, a []string or a comma-separated string; items are trimmed and
// empty items dropped. The result is always a fresh, non-nil slice.
func asList(raw any) ([]string, error) {
	switch v := raw.(type) {
	case []string:
		return cleanList(v), nil
	case []any:
		items := make([]string, 0, len(v))
		for _, it := range v {
			s, ok := it.(string)
			if !ok {
				return nil, fmt.Errorf("list items must be strings, got %T", it)
			}
			items = append(items, s)
		}
		return cleanList(items), nil
	case string:
		return cleanList(strings.Split(v, ",")), nil
	default:
		return nil, fmt.Errorf("expected a list, got %T", raw)
	}
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}
