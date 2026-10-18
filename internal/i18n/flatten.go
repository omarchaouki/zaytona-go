package i18n

import (
	"encoding/json"
	"fmt"
	"sort"
)

// Flatten turns nested message JSON into dotted keys ("hero.title" -> text).
// Every leaf must be a string. Exported for tools/i18ngen.
func Flatten(raw []byte) (map[string]string, error) {
	var tree map[string]any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, err
	}
	out := map[string]string{}
	var walk func(prefix string, node map[string]any) error
	walk = func(prefix string, node map[string]any) error {
		for k, v := range node {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			switch v := v.(type) {
			case string:
				out[key] = v
			case map[string]any:
				if err := walk(key, v); err != nil {
					return err
				}
			default:
				return fmt.Errorf("%s: leaf is %T, want string", key, v)
			}
		}
		return nil
	}
	return out, walk("", tree)
}

// Diff returns keys present in a but not in b, sorted.
func Diff(a, b map[string]string) []string {
	var missing []string
	for k := range a {
		if _, ok := b[k]; !ok {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	return missing
}
