// Package docsgen generates Markdown documentation from Go source code
// by parsing doc comments and type declarations.
package docsgen

import (
	"fmt"
	"go/doc"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type PackageDoc struct {
	Name       string    `json:"name"`
	ImportPath string    `json:"import_path"`
	Doc        string    `json:"doc"`
	Funcs      []FuncDoc `json:"funcs"`
	Types      []TypeDoc `json:"types"`
}

type FuncDoc struct {
	Name     string `json:"name"`
	Doc      string `json:"doc"`
	Sig      string `json:"signature"`
	IsMethod bool   `json:"is_method,omitempty"`
	Receiver string `json:"receiver,omitempty"`
}

type TypeDoc struct {
	Name    string    `json:"name"`
	Doc     string    `json:"doc"`
	Kind    string    `json:"kind"`
	Methods []FuncDoc `json:"methods,omitempty"`
}

type Generator struct{ Dir string }

func NewGenerator(dir string) *Generator { return &Generator{Dir: dir} }

func (g *Generator) GeneratePackage(importPath string) (*PackageDoc, error) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, importPath, nil, parser.ParseComments)
	if err != nil {
		return nil, fmt.Errorf("parse: %w", err)
	}
	pd := &PackageDoc{ImportPath: importPath}
	for name, pkg := range pkgs {
		pd.Name = name
		d := doc.New(pkg, importPath, doc.AllMethods)
		pd.Doc = d.Doc
		for _, fn := range d.Funcs {
			pd.Funcs = append(pd.Funcs, FuncDoc{Name: fn.Name, Doc: fn.Doc, Sig: fmt.Sprint(fn.Decl)})
		}
		for _, typ := range d.Types {
			td := TypeDoc{Name: typ.Name, Doc: typ.Doc}
			for _, m := range typ.Methods {
				td.Methods = append(td.Methods, FuncDoc{Name: m.Name, Doc: m.Doc, Receiver: typ.Name, IsMethod: true})
			}
			pd.Types = append(pd.Types, td)
		}
	}
	return pd, nil
}

func (g *Generator) GenerateDir(root string) ([]*PackageDoc, error) {
	var docs []*PackageDoc
	filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || !info.IsDir() {
			return nil
		}
		n := info.Name()
		if strings.HasPrefix(n, ".") || n == "vendor" || n == "node_modules" {
			return filepath.SkipDir
		}
		pd, err := g.GeneratePackage(path)
		if err != nil {
			return nil
		}
		if pd.Name != "" {
			docs = append(docs, pd)
		}
		return filepath.SkipDir
	})
	return docs, nil
}

func (g *Generator) FormatPackage(pd *PackageDoc) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Package %s\n\n`%s`\n\n%s\n\n", pd.Name, pd.ImportPath, pd.Doc)
	if len(pd.Funcs) > 0 {
		sb.WriteString("## Functions\n\n")
		for _, f := range pd.Funcs {
			fmt.Fprintf(&sb, "### %s\n\n```go\n%s\n```\n\n%s\n\n", f.Name, f.Sig, f.Doc)
		}
	}
	if len(pd.Types) > 0 {
		sb.WriteString("## Types\n\n")
		for _, t := range pd.Types {
			fmt.Fprintf(&sb, "### %s\n\n%s\n\n", t.Name, t.Doc)
			for _, m := range t.Methods {
				fmt.Fprintf(&sb, "- **%s**\n", m.Name)
			}
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func (g *Generator) FormatAll(docs []*PackageDoc) string {
	var sb strings.Builder
	sb.WriteString("# API Documentation\n\n")
	sort.Slice(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	for _, pd := range docs {
		first := pd.Doc
		if idx := strings.IndexByte(first, '\n'); idx > 0 {
			first = first[:idx]
		}
		fmt.Fprintf(&sb, "- [%s](%s) — %s\n", pd.Name, pd.ImportPath, first)
	}
	return sb.String()
}
