
package data

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/mesinkasir/gax/internal/parser"
)

func Load(dir string) map[string]interface{} {
	result := make(map[string]interface{})

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return result
	}

	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		path := filepath.Join(dir, e.Name())
		ext := strings.ToLower(filepath.Ext(e.Name()))
		name := strings.TrimSuffix(e.Name(), ext)

		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		var d interface{}
		if ext == ".json" {
			json.Unmarshal(b, &d)
		} else if ext == ".yaml" || ext == ".yml" {
			d = parser.ParseYAML(string(b))
		} else {
			continue
		}

		result[name] = d
	}

	return result
}