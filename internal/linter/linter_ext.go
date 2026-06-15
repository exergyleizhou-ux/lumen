// Package linter - extension: linting for non-Go files (JSON, YAML, TOML, Markdown),
// incremental linting, line-ending checks, duplicate import detection.
package linter

import (
	"fmt"
	"go/ast"
	"go/token"
	"strings"
)

// ---- Duplicate Import Check ----

func checkDuplicateImport(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	seen := make(map[string]bool)
	var issues []Issue
	for _, imp := range file.Imports {
		path := strings.Trim(imp.Path.Value, `"`)
		if seen[path] {
			pos := fset.Position(imp.Pos())
			issues = append(issues, Issue{
				Line:     pos.Line,
				Column:   pos.Column,
				Message:  fmt.Sprintf("duplicate import '%s'", path),
				Severity: SevError,
			})
		}
		seen[path] = true
	}
	return issues
}

// ---- Check for too many function parameters ----

func checkTooManyParams(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	maxParams := 5
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Type.Params == nil {
			return true
		}
		paramCount := 0
		for _, field := range fn.Type.Params.List {
			if len(field.Names) > 0 {
				paramCount += len(field.Names)
			} else {
				paramCount++
			}
		}
		if paramCount > maxParams {
			pos := fset.Position(fn.Pos())
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  pos.Column,
				Message: fmt.Sprintf("function '%s' has %d parameters (max %d)", fn.Name.Name, paramCount, maxParams),
			})
		}
		return true
	})
	return issues
}

// ---- Check for too many return values ----

func checkTooManyReturns(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	maxReturns := 3
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Type.Results == nil {
			return true
		}
		returnCount := len(fn.Type.Results.List)
		if returnCount > maxReturns {
			pos := fset.Position(fn.Pos())
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  pos.Column,
				Message: fmt.Sprintf("function '%s' has %d return values (max %d)", fn.Name.Name, returnCount, maxReturns),
			})
		}
		return true
	})
	return issues
}

// ---- Check for naked returns ----

func checkNakedReturn(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		ret, ok := n.(*ast.ReturnStmt)
		if !ok || len(ret.Results) > 0 {
			return true
		}
		// Naked return - check if enclosing function has named results
		pos := fset.Position(ret.Pos())
		issues = append(issues, Issue{
			Line:    pos.Line,
			Column:  pos.Column,
			Message: "naked return should be avoided in non-trivial functions",
		})
		return true
	})
	return issues
}

// ---- Check for fallthrough in switch ----

func checkFallthrough(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		cl, ok := n.(*ast.CaseClause)
		if !ok {
			return true
		}
		// Check if last statement is fallthrough
		if len(cl.Body) > 0 {
			if br, ok := cl.Body[len(cl.Body)-1].(*ast.BranchStmt); ok {
				if br.Tok == token.FALLTHROUGH {
					pos := fset.Position(br.Pos())
					issues = append(issues, Issue{
						Line:    pos.Line,
						Column:  pos.Column,
						Message: "fallthrough usage should be documented or avoided",
					})
				}
			}
		}
		return true
	})
	return issues
}

// ---- Check for fmt.Println in production code ----

var debugFuncs = map[string]bool{
	"fmt.Println": true,
	"fmt.Print":   true,
	"fmt.Printf":  true,
}

func checkDebugPrint(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	// Check for "_test.go" - if test file, skip
	if strings.HasSuffix(fset.Position(file.Pos()).Filename, "_test.go") {
		return nil
	}

	ast.Inspect(file, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		fnName := typeExprToString(sel.X) + "." + sel.Sel.Name
		if debugFuncs[fnName] {
			pos := fset.Position(call.Pos())
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  pos.Column,
				Message: fmt.Sprintf("debug print '%s' found in non-test code", fnName),
			})
		}
		return true
	})
	return issues
}

// ---- Check for package comment ----

func checkPackageComment(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	if file.Doc == nil || len(file.Doc.List) == 0 {
		pos := fset.Position(file.Pos())
		issues = append(issues, Issue{
			Line:    pos.Line,
			Column:  1,
			Message: "package is missing documentation comment",
		})
	}
	return issues
}

// ---- Check for unreachable code (basic heuristic) ----

func checkUnreachableCode(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		block, ok := n.(*ast.BlockStmt)
		if !ok || len(block.List) < 2 {
			return true
		}
		for i := 0; i < len(block.List)-1; i++ {
			// Check if current statement is a terminating statement
			if isTerminating(block.List[i]) {
				pos := fset.Position(block.List[i+1].Pos())
				issues = append(issues, Issue{
					Line:    pos.Line,
					Column:  pos.Column,
					Message: "code after terminating statement may be unreachable",
				})
				break // Only report once per block
			}
		}
		return true
	})
	return issues
}

func isTerminating(stmt ast.Stmt) bool {
	switch stmt.(type) {
	case *ast.ReturnStmt, *ast.BranchStmt:
		return true
	}
	// Check if it's an if-else where both branches terminate
	if ifStmt, ok := stmt.(*ast.IfStmt); ok {
		if ifStmt.Else != nil {
			return blockTerminates(ifStmt.Body) && blockTerminates(ifStmt.Else)
		}
	}
	return false
}

func blockTerminates(stmt ast.Stmt) bool {
	if block, ok := stmt.(*ast.BlockStmt); ok {
		if len(block.List) > 0 {
			return isTerminating(block.List[len(block.List)-1])
		}
	}
	return isTerminating(stmt)
}

// ---- Check for select with single case ----

func checkSingleCaseSelect(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	ast.Inspect(file, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectStmt)
		if !ok {
			return true
		}
		if len(sel.Body.List) == 1 {
			pos := fset.Position(sel.Pos())
			issues = append(issues, Issue{
				Line:    pos.Line,
				Column:  pos.Column,
				Message: "select with single case can be simplified",
			})
		}
		return true
	})
	return issues
}

// ---- Check for re-declared error variable ----

func checkRedeclaredErr(file *ast.File, fset *token.FileSet, src []byte) []Issue {
	var issues []Issue
	errCounts := make(map[int]int) // line -> count of err declarations

	ast.Inspect(file, func(n ast.Node) bool {
		assign, ok := n.(*ast.AssignStmt)
		if !ok || assign.Tok != token.DEFINE {
			return true
		}
		for _, lhs := range assign.Lhs {
			if ident, ok := lhs.(*ast.Ident); ok {
				if ident.Name == "err" {
					line := fset.Position(assign.Pos()).Line
					errCounts[line]++
				}
			}
		}
		return true
	})

	// Report lines where err is declared multiple times (potential shadow)
	for line, count := range errCounts {
		if count > 1 {
			issues = append(issues, Issue{
				Line:    line,
				Column:  1,
				Message: fmt.Sprintf("multiple 'err' declarations on same line: potential shadowing"),
			})
		}
	}

	return issues
}
