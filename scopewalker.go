package main

import (
	"go/ast"
	"go/token"
	"log"
)

type ScopeVisitor interface {
	VisitExpr(scope *ast.Scope, expr ast.Expr) (w ScopeVisitor)
	VisitStmt(scope *ast.Scope, stmt ast.Stmt) (w ScopeVisitor)
	ExitScope(scope *ast.Scope) (w ScopeVisitor)
}

// We traverse types, since we need them to determine if import is used
func WalkFields(v ScopeVisitor, fields []*ast.Field, scope *ast.Scope) {
	for _, field := range fields {
		for _, name := range field.Names {
			insertToScope(scope, name.Obj)
		}
		WalkExpr(v, field.Type, scope)
	}
}

func WalkExpr(v ScopeVisitor, expr ast.Expr, scope *ast.Scope) {
	if v = v.VisitExpr(scope, expr); v == nil {
		return
	}
	switch expr := expr.(type) {
	case *ast.Ellipsis:
		if expr.Elt != nil {
			WalkExpr(v, expr.Elt, scope)
		}
	case *ast.FuncLit:
		newscope := ast.NewScope(scope)
		if expr.Type.Params != nil {
			WalkFields(v, expr.Type.Params.List, newscope)
		}
		if expr.Type.Results != nil {
			WalkFields(v, expr.Type.Results.List, newscope)
		}
		WalkStmt(v, expr.Body, newscope)
		v.ExitScope(newscope)
	case *ast.BadExpr:
		// nothing to do
	case *ast.ParenExpr:
		WalkExpr(v, expr.X, scope)
	case *ast.SelectorExpr:
		WalkExpr(v, expr.X, scope)
		WalkExpr(v, expr.Sel, scope)
	case *ast.IndexExpr:
		WalkExpr(v, expr.X, scope)
		WalkExpr(v, expr.Index, scope)
	case *ast.SliceExpr:
		WalkExpr(v, expr.X, scope)
		if expr.Low != nil {
			WalkExpr(v, expr.Low, scope)
		}
		if expr.High != nil {
			WalkExpr(v, expr.High, scope)
		}
	case *ast.TypeAssertExpr:
		WalkExpr(v, expr.X, scope)
		if expr.Type != nil {
			WalkExpr(v, expr.Type, scope)
		}
	case *ast.CallExpr:
		WalkExpr(v, expr.Fun, scope)
		for _, e := range expr.Args {
			WalkExpr(v, e, scope)
		}
	case *ast.StarExpr:
		WalkExpr(v, expr.X, scope)
	case *ast.UnaryExpr:
		WalkExpr(v, expr.X, scope)
	case *ast.BinaryExpr:
		WalkExpr(v, expr.X, scope)
		WalkExpr(v, expr.Y, scope)
	case *ast.KeyValueExpr:
		WalkExpr(v, expr.Key, scope)
		WalkExpr(v, expr.Value, scope)
	}
}

func WalkStmt(v ScopeVisitor, stmt ast.Stmt, scope *ast.Scope) (newscope *ast.Scope) {
	newscope = scope
	if v = v.VisitStmt(scope, stmt); v == nil {
		return
	}
	switch stmt := stmt.(type) {
	case *ast.ExprStmt:
		WalkExpr(v, stmt.X, scope)
	case *ast.IncDecStmt:
		WalkExpr(v, stmt.X, scope)
	case *ast.ReturnStmt:
		for _, expr := range stmt.Results {
			WalkExpr(v, expr, scope)
		}
	case *ast.AssignStmt:
		if stmt.Tok == token.DEFINE {
			newscope = ast.NewScope(scope)
			for _, expr := range stmt.Rhs {
				WalkExpr(v, expr, scope)
			}
			for _, expr := range stmt.Lhs {
				insertToScope(newscope, expr.(*ast.Ident).Obj)
			}
		} else {
			for _, expr := range stmt.Lhs {
				WalkExpr(v, expr, scope)
			}
			for _, expr := range stmt.Rhs {
				WalkExpr(v, expr, scope)
			}
		}
	case *ast.DeclStmt:
		switch decl := stmt.Decl.(type) {
		case *ast.GenDecl:
			for _, spec := range decl.Specs {
				newscope = ast.NewScope(newscope)
				switch spec := spec.(type) {
				case *ast.TypeSpec:
					insertToScope(newscope, spec.Name.Obj)
					WalkExpr(v, spec.Type, scope)
				case *ast.ValueSpec:
					for _, name := range spec.Names {
						insertToScope(newscope, name.Obj)
					}
					for _, value := range spec.Values {
						WalkExpr(v, value, scope)
					}
				default:
					panic("cannot have an import in a statement (or so I hope)")
				}
			}
		default:
			panic("only GenDecl can appear in statement")
		}
	case *ast.SendStmt:
		WalkExpr(v, stmt.Chan, scope)
		WalkExpr(v, stmt.Value, scope)
	case *ast.DeferStmt:
		WalkExpr(v, stmt.Call, scope)
	case *ast.GoStmt:
		WalkExpr(v, stmt.Call, scope)
	case *ast.LabeledStmt:
		WalkStmt(v, stmt.Stmt, scope)
	case *ast.BranchStmt:
		// nothing to do
	case *ast.IfStmt:
		inner := scope
		if stmt.Init != nil {
			inner = WalkStmt(v, stmt.Init, inner)
		}
		WalkExpr(v, stmt.Cond, scope)
		WalkStmt(v, stmt.Body, inner)
		if stmt.Else != nil {
			WalkStmt(v, stmt.Else, inner)
		}
		exitScopes(v, inner, scope)
	case *ast.ForStmt:
		inner := scope
		if stmt.Init != nil {
			inner = WalkStmt(v, stmt.Init, inner)
		}
		if stmt.Cond != nil {
			WalkExpr(v, stmt.Cond, scope)
		}
		if stmt.Post != nil {
			WalkStmt(v, stmt.Post, scope)
		}
		WalkStmt(v, stmt.Body, scope)
		exitScopes(v, inner, scope)
	case *ast.RangeStmt:
		inner := scope
		if stmt.Tok == token.ASSIGN {
			WalkExpr(v, stmt.Key, scope)
			WalkExpr(v, stmt.Value, scope)
		} else if stmt.Tok == token.DEFINE {
			inner = ast.NewScope(inner)
			insertToScope(inner, stmt.Key.(*ast.Ident).Obj)
			if stmt.Value != nil {
				insertToScope(inner, stmt.Value.(*ast.Ident).Obj)
			}
		} else {
			panic("range statement must have := or = token")
		}
		WalkStmt(v, stmt.Body, scope)
		exitScopes(v, inner, scope)
	case *ast.CaseClause:
		inner := ast.NewScope(scope)
		for _, expr := range stmt.List {
			WalkExpr(v, expr, scope)
		}
		for _, s := range stmt.Body {
			inner = WalkStmt(v, s, inner)
		}
		exitScopes(v, inner, scope)
	case *ast.SwitchStmt:
		inner := scope
		if stmt.Init != nil {
			inner = WalkStmt(v, stmt.Init, inner)
		}
		WalkExpr(v, stmt.Tag, scope)
		WalkStmt(v, stmt.Body, inner)
		exitScopes(v, inner, scope)
	case *ast.TypeSwitchStmt:
		inner := scope
		if stmt.Init != nil {
			inner = WalkStmt(v, stmt.Init, inner)
		}
		inner = WalkStmt(v, stmt.Assign, inner)
		WalkStmt(v, stmt.Body, inner)
		exitScopes(v, inner, scope)
	case *ast.CommClause:
		inner := WalkStmt(v, stmt.Comm, scope)
		for _, s := range stmt.Body {
			inner = WalkStmt(v, s, inner)
		}
		exitScopes(v, inner, scope)
	case *ast.SelectStmt:
		WalkStmt(v, stmt.Body, scope)
	case *ast.BlockStmt:
		inner := ast.NewScope(scope)
		for _, s := range stmt.List {
			inner = WalkStmt(v, s, inner)
		}
		exitScopes(v, inner, scope)
	default:
		log.Fatalf("Cannot understand %+#v", stmt)
	}
	return
}

func exitScopes(v ScopeVisitor, inner, limit *ast.Scope) {
	for inner != limit {
		if inner == nil {
			panic("exitScopes must be bounded")
		}
		v.ExitScope(inner)
		inner = inner.Outer
	}
}

func WalkFile(v ScopeVisitor, file *ast.File) {
	if v == nil {
		return
	}
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			scope := ast.NewScope(file.Scope)
			if d.Recv != nil {
				insertToScope(scope, d.Recv.List[0].Names[0].Obj)
			}
			WalkFields(v, d.Type.Params.List, scope)
			// see http://golang.org/ref/spec#Function_declarations
			// "A function declaration may omit the body.
			//  Such a declaration provides the signature for a function implemented outside Go,
			//  such as an assembly routine."
			// for example sigpipe at os/file_posix.go
			if d.Body != nil {
				WalkStmt(v, d.Body, scope)
			}
			v.ExitScope(scope)
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				switch spec := spec.(type) {
				case *ast.ValueSpec:
					// already in scope insertToScope(file.Scope, spec.Names)
					for _, value := range spec.Values {
						WalkExpr(v, value, file.Scope)
					}
				}
			}
		}
	}
	v.ExitScope(file.Scope)
}

func insertToScope(scope *ast.Scope, obj *ast.Object) {
	if obj.Name == "_" {
		return
	}
	if obj.Kind == ast.Fun && obj.Name == "init" {
		return
	}
	scope.Insert(obj)
}
