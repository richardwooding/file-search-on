package goresolve

import (
	"go/ast"
	"go/types"

	"golang.org/x/tools/go/packages"
)

// collectEdges records a call-graph edge for every call site in the package:
// from the enclosing function (callerID; "" for calls in package-level
// initialisers) to the precisely-resolved callee. A call through an
// interface resolves to the interface method — correct for who_calls
// ("who calls THIS exact symbol"), unlike the name-based tool which matches
// every same-named symbol.
func (r *Result) collectEdges(p *packages.Package) {
	info := p.TypesInfo
	record := func(callerID string, call *ast.CallExpr) {
		var obj types.Object
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			obj = info.Uses[fn]
		case *ast.SelectorExpr:
			obj = info.Uses[fn.Sel]
		}
		fn, ok := obj.(*types.Func)
		if !ok {
			return
		}
		calleeID := funcID(fn)
		if calleeID == "" {
			return
		}
		set := r.callers[calleeID]
		if set == nil {
			set = map[string]bool{}
			r.callers[calleeID] = set
		}
		set[callerID] = true
		pos := p.Fset.Position(call.Pos())
		caller := ""
		if d, ok := r.defByID[callerID]; ok {
			caller = d.Qualified()
		}
		r.sites[calleeID] = append(r.sites[calleeID], CallSite{Path: pos.Filename, Line: pos.Line, Caller: caller})
	}

	for _, f := range p.Syntax {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				// Calls in package var initialisers etc. — caller "".
				ast.Inspect(decl, func(n ast.Node) bool {
					if call, ok := n.(*ast.CallExpr); ok {
						record("", call)
					}
					return true
				})
				continue
			}
			// All calls in the body (including closures) attribute to fd —
			// matches the name-based extractor's call attribution.
			callerID := ""
			if obj, ok := info.Defs[fd.Name].(*types.Func); ok {
				callerID = funcID(obj)
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				if call, ok := n.(*ast.CallExpr); ok {
					record(callerID, call)
				}
				return true
			})
		}
	}
}

// idsMatching returns the resolved symbol ids of every definition named
// `name` whose owner matches (owner=="" matches any owner — so a bare query
// covers a function and all same-named methods, each still resolved per
// call site).
func (r *Result) idsMatching(owner, name string) []string {
	var ids []string
	for id, d := range r.defByID {
		if d.Name == name && (owner == "" || d.Owner == owner) {
			ids = append(ids, id)
		}
	}
	return ids
}

// Callers returns the resolved call sites of the queried symbol — the
// type-precise who_calls. owner may be "" to match any owner of `name`.
func (r *Result) Callers(owner, name string) []CallSite {
	var out []CallSite
	seen := map[string]bool{}
	for _, id := range r.idsMatching(owner, name) {
		for _, s := range r.sites[id] {
			key := s.Path + "\x00" + s.Caller
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, s)
		}
	}
	return out
}

// ImpactSym is a transitive caller plus its BFS distance (hops) from the
// queried symbol.
type ImpactSym struct {
	Symbol
	Depth int
}

// Impact returns the transitive reverse-call closure of the queried symbol:
// every function that (in)directly calls it — the blast radius — each with
// its hop distance. owner may be "" to match any owner of `name`.
func (r *Result) Impact(owner, name string) []ImpactSym {
	visited := map[string]bool{}
	type qi struct {
		id    string
		depth int
	}
	var queue []qi
	for _, id := range r.idsMatching(owner, name) {
		visited[id] = true
		queue = append(queue, qi{id, 0})
	}
	var out []ImpactSym
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for callerID := range r.callers[cur.id] {
			if callerID == "" || visited[callerID] {
				continue
			}
			visited[callerID] = true
			queue = append(queue, qi{callerID, cur.depth + 1})
			if d, ok := r.defByID[callerID]; ok {
				out = append(out, ImpactSym{Symbol: d, Depth: cur.depth + 1})
			}
		}
	}
	return out
}
