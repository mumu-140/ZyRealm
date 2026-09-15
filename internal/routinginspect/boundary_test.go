package routinginspect

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestRoutingInspectorDoesNotDependOnLiveDataPlane(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read routinginspect package: %v", err)
	}

	forbidden := []string{
		"github.com/bestruirui/octopus/internal/relay",
		"github.com/bestruirui/octopus/internal/availability",
		"github.com/bestruirui/octopus/internal/db",
		"github.com/bestruirui/octopus/internal/op",
		"github.com/bestruirui/octopus/internal/server",
	}

	files := token.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		path := filepath.Join(".", entry.Name())
		parsed, err := parser.ParseFile(files, path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, imported := range parsed.Imports {
			importPath, err := strconv.Unquote(imported.Path.Value)
			if err != nil {
				t.Fatalf("unquote import %s in %s: %v", imported.Path.Value, path, err)
			}
			for _, prefix := range forbidden {
				if importPath == prefix || strings.HasPrefix(importPath, prefix+"/") {
					t.Fatalf("routing inspector must stay historical/read-only: %s imports forbidden live data-plane package %s", path, importPath)
				}
			}
		}
	}
}
