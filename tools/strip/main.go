// Command strip produces the published source tree that the lobby serves
// at /source/*. It walks an allowlist of repo paths (see roots() below),
// transforms each file per its extension, and writes the result under
// -out. Anything outside the allowlist is never copied — that's the
// canonical defence against leaking lobby code, tools/, docs, or build
// configs that would reveal the bug-hiding strategy.
//
// Per-file transforms:
//   - .go         parsed with go/ast; comments + doc strings dropped;
//                 build-tag comments stripped; output via go/printer
//                 (gofmt-compatible). _test.go files skipped entirely.
//   - .ts / .tsx  state-machine strip of // and /* */ comments while
//                 preserving content of "" / '' / `` strings. *.test.ts
//                 and *.test.tsx files skipped.
//   - everything else (.json, .html, .css, .mod, .sum, ...) copied
//     verbatim — they don't carry doc comments that would expose
//     intentional-bug context.
//
// Usage:
//
//	cd tools/strip && go run . -in ../.. -out /tmp/stripped
//
// Spec: docs/specs/foundations/09-ci-cd-deploy.md#source-code-publishing-served-by-lobby.
package main

import (
	"bufio"
	"embed"
	"flag"
	"fmt"
	"go/ast"
	"go/build/constraint"
	"go/parser"
	"go/printer"
	"go/token"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// stubsFS holds files that are written verbatim into the published tree
// after the allowlist walk. Today this is `pkg/identity` — the real
// package is private (see docs/specs/foundations/09-ci-cd-deploy.md), so
// the published tarball ships a no-auth stub that lets `server/`
// compile and run locally for puzzle research.
//
//go:embed all:stubs
var stubsFS embed.FS

func main() {
	in := flag.String("in", ".", "input source root (the repo root)")
	out := flag.String("out", "stripped-source", "output directory")
	flag.Parse()

	log.SetFlags(0)

	if err := run(*in, *out); err != nil {
		log.Fatalf("strip: %v", err)
	}
}

// roots is the published-paths allowlist. Each entry is interpreted
// relative to -in; missing entries are silently skipped (so the tool
// stays tolerant of repo refactors that drop one of these dirs).
//
// Keep this list in lockstep with docs/specs/foundations/09-ci-cd-deploy.md.
func roots() []string {
	return []string{
		"server",
		"pkg/replayauth",
		"client/src",
		"client/public",
		"client/vite",
		"client/index.html",
		"client/spectate.html",
		"client/package.json",
		"client/tsconfig.json",
		"client/style.css",
		"client/yarn.lock",
	}
}

func run(in, out string) error {
	absIn, err := filepath.Abs(in)
	if err != nil {
		return fmt.Errorf("resolve in: %w", err)
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return fmt.Errorf("mkdir out: %w", err)
	}

	for _, root := range roots() {
		src := filepath.Join(absIn, root)
		info, err := os.Stat(src)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("stat %s: %w", root, err)
		}
		if !info.IsDir() {
			if err := processFile(src, filepath.Join(out, root)); err != nil {
				return fmt.Errorf("%s: %w", root, err)
			}
			continue
		}
		err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			rel, relErr := filepath.Rel(absIn, path)
			if relErr != nil {
				return relErr
			}
			return processFile(path, filepath.Join(out, rel))
		})
		if err != nil {
			return fmt.Errorf("%s: %w", root, err)
		}
	}
	return copyStubs(out)
}

// copyStubs writes the embedded stubs/ tree into out verbatim. Stubs
// must NOT pass through processFile — the explanatory header on the
// stub identity package is the whole point of shipping it, and the Go
// stripper would erase it.
//
// `.tmpl` suffixes in the embedded tree are stripped on copy: the
// stub `pkg/go.mod` is stored as `go.mod.tmpl` in source so Go's
// build doesn't treat the stub dir as a nested module (which would
// make the embed itself fail).
func copyStubs(out string) error {
	return fs.WalkDir(stubsFS, "stubs", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel := strings.TrimSuffix(strings.TrimPrefix(p, "stubs/"), ".tmpl")
		dst := filepath.Join(out, rel)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		data, err := stubsFS.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
}

func processFile(srcPath, dstPath string) error {
	base := filepath.Base(srcPath)
	switch {
	case strings.HasSuffix(base, "_test.go"):
		return nil
	case strings.HasSuffix(base, ".test.ts"), strings.HasSuffix(base, ".test.tsx"):
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	switch filepath.Ext(srcPath) {
	case ".go":
		return stripGoFile(srcPath, dstPath)
	case ".ts", ".tsx":
		return stripTSFile(srcPath, dstPath)
	default:
		return copyFile(srcPath, dstPath)
	}
}

// stripGoFile parses src as Go, drops every comment and doc string, and
// re-prints via go/printer. The build-tag comment (//go:build … line) is
// part of the file's comment list and gets dropped along with the rest;
// that's the spec'd behaviour — published source must compile but
// without revealing the `!norace` directive that hides BUG-11.
//
// Files that are NOT part of the default build (i.e. whose //go:build
// constraint evaluates to false with no extra tags) are skipped entirely
// — typical case is the `killlog_norace.go` safe-variant that only
// compiles under `go test -race -tags=norace`. Keeping it would
// double-declare against the prod variant once both lose their tags.
func stripGoFile(src, dst string) error {
	keep, err := defaultBuildKeepsFile(src)
	if err != nil {
		return fmt.Errorf("build constraint %s: %w", src, err)
	}
	if !keep {
		return nil
	}

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, src, nil, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse %s: %w", src, err)
	}
	f.Comments = keepGoDirectiveGroups(f.Comments)
	f.Doc = filterGoDirectives(f.Doc)
	stripDeclDocs(f)

	out, createErr := os.Create(dst)
	if createErr != nil {
		return createErr
	}
	defer out.Close()

	cfg := &printer.Config{Mode: printer.UseSpaces | printer.TabIndent, Tabwidth: 4}
	if err := cfg.Fprint(out, fset, f); err != nil {
		return fmt.Errorf("print %s: %w", dst, err)
	}
	if _, err := out.Write([]byte{'\n'}); err != nil {
		return err
	}
	return nil
}

// defaultBuildKeepsFile reports whether the file's //go:build constraint
// (if any) is satisfied with no extra tags. A file with no build line
// always keeps; a file that requires a tag like `norace` is dropped.
func defaultBuildKeepsFile(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		if !constraint.IsGoBuild(line) {
			continue
		}
		expr, err := constraint.Parse(line)
		if err != nil {
			return false, err
		}
		// Default build: no tags set; every tag query returns false.
		return expr.Eval(func(tag string) bool { return false }), nil
	}
	return true, scanner.Err()
}

// filterGoDirectives strips non-//go: lines from cg in place and
// returns cg, or nil if no //go: lines remain. Mutating in place
// preserves the pointer identity that go/printer uses to match
// f.Comments entries against ast.Decl.Doc fields.
func filterGoDirectives(cg *ast.CommentGroup) *ast.CommentGroup {
	if cg == nil {
		return nil
	}
	var kept []*ast.Comment
	for _, c := range cg.List {
		if strings.HasPrefix(c.Text, "//go:") {
			kept = append(kept, c)
		}
	}
	if len(kept) == 0 {
		return nil
	}
	cg.List = kept
	return cg
}

// keepGoDirectiveGroups filters f.Comments down to groups that contain
// at least one //go: pragma (non-pragma lines already removed in place
// by filterGoDirectives).
func keepGoDirectiveGroups(comments []*ast.CommentGroup) []*ast.CommentGroup {
	var kept []*ast.CommentGroup
	for _, cg := range comments {
		if filterGoDirectives(cg) != nil {
			kept = append(kept, cg)
		}
	}
	return kept
}

// stripDeclDocs walks top-level declarations and removes their attached
// doc strings, preserving any //go: pragma lines (e.g. //go:embed).
func stripDeclDocs(f *ast.File) {
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			d.Doc = filterGoDirectives(d.Doc)
			for _, spec := range d.Specs {
				switch s := spec.(type) {
				case *ast.ValueSpec:
					s.Doc = filterGoDirectives(s.Doc)
					s.Comment = nil
				case *ast.TypeSpec:
					s.Doc = filterGoDirectives(s.Doc)
					s.Comment = nil
					if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
						for _, fld := range st.Fields.List {
							fld.Doc = filterGoDirectives(fld.Doc)
							fld.Comment = nil
						}
					}
					if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
						for _, fld := range it.Methods.List {
							fld.Doc = filterGoDirectives(fld.Doc)
							fld.Comment = nil
						}
					}
				case *ast.ImportSpec:
					s.Doc = filterGoDirectives(s.Doc)
					s.Comment = nil
				}
			}
		case *ast.FuncDecl:
			d.Doc = filterGoDirectives(d.Doc)
		}
	}
}

// stripTSFile is a state-machine comment stripper for .ts / .tsx. It
// preserves the contents of "" / '' / `` strings (including escaped
// quotes), and drops // line comments and /* */ block comments outside
// strings. Edge cases this deliberately doesn't handle: comments inside
// `${...}` template-literal expression interpolations (rare in our
// codebase) and regex literals where `//` happens to follow `/`. Both
// have been audited and don't currently appear in client/src/.
func stripTSFile(src, dst string) error {
	in, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	stripped := stripTSComments(string(in))
	return os.WriteFile(dst, []byte(stripped), 0o644)
}

type tsState int

const (
	tsCode tsState = iota
	tsLineComment
	tsBlockComment
	tsDouble
	tsSingle
	tsTemplate
)

func stripTSComments(src string) string {
	var b strings.Builder
	state := tsCode
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case tsCode:
			if c == '/' && i+1 < len(src) {
				next := src[i+1]
				if next == '/' {
					state = tsLineComment
					i++
					continue
				}
				if next == '*' {
					state = tsBlockComment
					i++
					continue
				}
			}
			b.WriteByte(c)
			switch c {
			case '"':
				state = tsDouble
			case '\'':
				state = tsSingle
			case '`':
				state = tsTemplate
			}
		case tsLineComment:
			if c == '\n' {
				state = tsCode
				b.WriteByte(c)
			}
		case tsBlockComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				state = tsCode
				i++
			}
		case tsDouble:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(src[i+1])
				i++
				continue
			}
			if c == '"' {
				state = tsCode
			}
		case tsSingle:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(src[i+1])
				i++
				continue
			}
			if c == '\'' {
				state = tsCode
			}
		case tsTemplate:
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				b.WriteByte(src[i+1])
				i++
				continue
			}
			if c == '`' {
				state = tsCode
			}
		}
	}
	return b.String()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

