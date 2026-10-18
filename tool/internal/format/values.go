package format

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Values reads a convention's one ```values block. The bool says whether one was
// present. A fence at the left margin declares; an indented one is an example and is
// ignored, which is what lets the rule be written inside a list item.
func Values(body string) (map[string]string, bool, error) {
	lines := strings.Split(body, "\n")
	var out map[string]string
	found := false
	for i := 0; i < len(lines); i++ {
		if lines[i] != "```values" {
			continue
		}
		if found {
			return nil, true, fmt.Errorf("a convention has one values block")
		}
		found = true
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if lines[j] == "```" {
				end = j
				break
			}
		}
		if end < 0 {
			return nil, true, fmt.Errorf("the values block is not closed")
		}
		var raw map[string]any
		if err := yaml.Unmarshal([]byte(strings.Join(lines[i+1:end], "\n")), &raw); err != nil {
			return nil, true, fmt.Errorf("the values block: %w", err)
		}
		out = map[string]string{}
		for k, v := range raw {
			switch x := v.(type) {
			case string:
				out[k] = x
			case int, int64, float64, bool:
				out[k] = fmt.Sprint(x)
			case nil:
				return nil, true, fmt.Errorf("value %q is empty", k)
			default:
				return nil, true, fmt.Errorf("value %q is not a scalar", k)
			}
		}
		i = end
	}
	return out, found, nil
}

// ParseWindow parses a collect.window value: a Go duration, or a whole number of
// days (d) or weeks (w).
func ParseWindow(s string) (time.Duration, error) {
	if s == "" {
		return 0, nil
	}
	if n, ok := strings.CutSuffix(s, "d"); ok {
		v, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("collect.window %q is not a duration", s)
		}
		return time.Duration(v) * 24 * time.Hour, nil
	}
	if n, ok := strings.CutSuffix(s, "w"); ok {
		v, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("collect.window %q is not a duration", s)
		}
		return time.Duration(v) * 7 * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("collect.window %q is not a duration", s)
	}
	return d, nil
}
