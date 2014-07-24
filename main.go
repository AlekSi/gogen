package main

import (
	"flag"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var re = regexp.MustCompile(`(?i)_type[^\W_]*_`)

const removeMarker = "**REMOVE**"

// Inspect an AST in depth-first order, replacing identifiers with mark.
// Returns actual replacements.
func mark(fileNode *ast.File, mapping map[string]string) map[string]string {
	res := make(map[string]string)

	ast.Inspect(fileNode, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.TypeSpec:
			if mapping[n.Name.Name] != "" {
				// This is our template type declaration, replace name with marker.
				n.Name.Name = removeMarker
			}

		case *ast.Ident:
			// Replace identifier name according to our mapping.
			n.Name = re.ReplaceAllStringFunc(n.Name, func(name string) string {
				r := mapping[name]
				if r == "" {
					log.Fatalf("no mapping for %q", name[1:len(name)-1])
				}
				res[name] = r
				return r
			})
		}

		return true
	})

	return res
}

// Iterate over AST nodes and remove marked declarations.
func sweep(fileNode *ast.File, mapping map[string]string) {
	decls := make([]ast.Decl, 0, len(fileNode.Decls))
	for _, decl := range fileNode.Decls {
		switch gd := decl.(type) {
		case *ast.GenDecl:
			specs := make([]ast.Spec, 0, len(gd.Specs))
			for _, spec := range gd.Specs {
				switch ts := spec.(type) {
				case *ast.TypeSpec:
					if ts.Name.Name == removeMarker {
						continue
					}
				}

				specs = append(specs, spec)
			}

			if len(specs) == 0 {
				continue
			}
			gd.Specs = specs
		}

		decls = append(decls, decl)
	}
	fileNode.Decls = decls
}

func outputFileName(file string, replaced map[string]string, types []string) string {
	ext := filepath.Ext(file)
	base := filepath.Base(strings.TrimSuffix(file, ext))
	if strings.HasSuffix(base, "_test") {
		base = strings.TrimSuffix(base, "_test")
		ext = "_test" + ext
	}

	for _, t := range types {
		if replaced[t] != "" {
			base += "_" + strings.ToLower(replaced[t])
		}
	}
	return base + ext
}

func main() {
	log.SetFlags(0)

	flag.Usage = func() {
		log.Printf("Usage: %s [mappings] [files or packages]:", os.Args[0])
		flag.PrintDefaults()
		log.Print("\nExample:")
		log.Printf("%s typeKey=int typeValue=string github.com/AlekSi/gogen-library/map",
			os.Args[0])
	}
	flag.Parse()

	mapping := make(map[string]string)
	var types []string
	args := flag.Args()
	for len(args) > 0 {
		arg := args[0]
		if !strings.Contains(arg, "=") {
			break
		}

		p := strings.SplitN(arg, "=", 2)
		types = append(types, "_"+p[0]+"_")
		mapping["_"+p[0]+"_"] = p[1]
		mapping["_"+strings.Title(p[0])+"_"] = strings.Title(p[1])

		args = args[1:]
	}

	if len(args) == 0 {
		// package in current directory
		args = []string{"."}
	}
	for _, arg := range args {
		log.SetPrefix(arg + ": ")

		if strings.HasPrefix(arg, "-") {
			log.Fatal("flags should be given before arguments")
		}

		var files []string
		if strings.HasSuffix(arg, ".go") {
			files = []string{arg}
		} else {
			wd, err := os.Getwd()
			if err != nil {
				log.Fatal(err)
			}
			pack, err := build.Import(arg, wd, 0)
			if err != nil {
				log.Fatal(err)
			}
			files = append(files, pack.GoFiles...)
			files = append(files, pack.CgoFiles...)
			files = append(files, pack.TestGoFiles...)
			files = append(files, pack.XTestGoFiles...)
			for i, file := range files {
				files[i] = filepath.Join(pack.Dir, file)
			}
		}

		for _, file := range files {
			log.SetPrefix(file + ": ")

			fset := token.NewFileSet()
			fileNode, err := parser.ParseFile(fset, file, nil, parser.ParseComments)
			if err != nil {
				log.Fatal(err)
			}

			replaced := mark(fileNode, mapping)
			sweep(fileNode, mapping)
			// ast.Print(fset, fileNode)

			out := outputFileName(file, replaced, types)
			f, err := os.Create(out)
			if err != nil {
				log.Fatal(err)
			}
			printer.Fprint(f, fset, fileNode)
			f.Close()
		}
	}
}
