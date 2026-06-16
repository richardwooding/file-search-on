// Package goresolve provides precise, type-checked Go symbol resolution
// via golang.org/x/tools/go/packages (issue #447, Phase 3 of #443).
//
// Unlike the name-based code graph (which matches by bare identifier and
// over-matches across same-named methods/types), this resolves each call
// to the exact *types.Func it binds to — distinguishing (*A).Foo from
// (*B).Foo and following cross-package usage precisely.
//
// COST / REQUIREMENTS: go/packages shells out to the `go` toolchain and
// needs a buildable module (deps resolvable). It therefore only works in a
// development environment — NOT in the chainguard/static OCI image or for
// users without Go installed. Callers treat it as an OPT-IN accuracy mode
// (--resolve / resolve:true) and MUST degrade to the name-based graph when
// Available reports false. Heavier than tree-sitter (~seconds per module),
// which is why it is never the default.
package goresolve

import (
	"context"
	"go/types"
	"os/exec"
	"strings"

	"golang.org/x/tools/go/packages"
)

// Symbol is a resolved Go function or method definition.
type Symbol struct {
	Pkg   string // package import path
	Owner string // receiver type name for methods; "" for plain funcs
	Name  string
	Path  string // file containing the definition
	Line  int
}

// Qualified renders the disambiguated name: "Owner.Name" for methods,
// "Name" for plain functions.
func (s Symbol) Qualified() string {
	if s.Owner != "" {
		return s.Owner + "." + s.Name
	}
	return s.Name
}

// Available reports whether type resolution can run here: the `go` toolchain
// must be on PATH (go/packages drives it). Callers use this to decide
// whether to attempt Resolve or go straight to the name-based fallback.
func Available() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

// Result is the type-resolved view of a Go module: every function/method
// definition, and the set of those that are referenced anywhere (calls,
// method values, etc.), keyed by a stable qualified id.
type Result struct {
	Defs       []Symbol
	referenced map[string]bool // id() of every used *types.Func
	// Call graph (#447 who_calls/impact): per-callee call sites and the
	// reverse edge set (callee id -> set of caller ids). callerID is "" for
	// calls outside any function (package var initialisers). Callees are
	// resolved precisely, so a call through an interface is attributed to
	// the interface method, not the concrete impl.
	callers map[string]map[string]bool // calleeID -> set of callerID
	sites   map[string][]CallSite      // calleeID -> call-site locations
	defByID map[string]Symbol          // id -> definition (for Impact output)
}

// CallSite is one resolved call to a queried symbol.
type CallSite struct {
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Caller string `json:"caller,omitempty"` // qualified enclosing func; "" if file-level
}

// DeadFuncs returns the definitions whose resolved symbol is never
// referenced — precise candidate dead code (no same-name collisions;
// cross-package, cross-type, and interface-dispatch usage are all counted;
// init / main / test entry points excluded). Two residual false-positive
// classes are inherent to static analysis and shared with the name-based
// tool: (1) exported API used only by EXTERNAL callers (a library's public
// surface looks unused from inside), and (2) methods reached only by
// REFLECTION / dynamic dispatch (e.g. kong/cobra command handlers, plugin
// registries) — go/types can't see those call sites.
func (r *Result) DeadFuncs() []Symbol {
	var dead []Symbol
	for _, d := range r.Defs {
		if !r.referenced[symbolID(d.Pkg, d.Owner, d.Name)] {
			dead = append(dead, d)
		}
	}
	return dead
}

func symbolID(pkg, owner, name string) string {
	return pkg + "\x00" + owner + "\x00" + name
}

// funcID derives the stable id of a resolved *types.Func (method or plain
// function), matching the id of its definition Symbol.
func funcID(fn *types.Func) string {
	if fn == nil || fn.Pkg() == nil {
		return ""
	}
	owner := ""
	if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
		owner = recvTypeName(sig.Recv().Type())
	}
	return symbolID(fn.Pkg().Path(), owner, fn.Name())
}

// recvTypeName returns the base name of a receiver type, unwrapping a
// pointer and a generic instantiation ("*Gen[T]" → "Gen").
func recvTypeName(t types.Type) string {
	if ptr, ok := t.(*types.Pointer); ok {
		t = ptr.Elem()
	}
	switch n := t.(type) {
	case *types.Named:
		return n.Obj().Name()
	case *types.Alias:
		return n.Obj().Name()
	}
	return ""
}

// Resolve type-checks the Go module rooted at dir and returns its resolved
// definitions + reference set. ok is false (with nil Result, nil error)
// when resolution isn't possible here — no toolchain, not a Go module, or
// no packages loaded — so callers degrade to the name-based graph rather
// than treating an empty result as "everything is dead". A non-nil error is
// returned only for unexpected loader failures.
func Resolve(ctx context.Context, dir string) (*Result, bool, error) {
	if !Available() {
		return nil, false, nil
	}
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedSyntax | packages.NeedTypes | packages.NeedTypesInfo |
			packages.NeedImports | packages.NeedDeps,
		Dir:     dir,
		Context: ctx,
		// Load test variants so a function used only from a _test.go counts
		// as referenced (else it reads as dead). Test files themselves are
		// excluded from the dead set below.
		Tests: true,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, false, err
	}
	if len(pkgs) == 0 {
		return nil, false, nil
	}

	res := &Result{
		referenced: map[string]bool{},
		callers:    map[string]map[string]bool{},
		sites:      map[string][]CallSite{},
		defByID:    map[string]Symbol{},
	}
	seenDef := map[string]bool{} // dedup defs across normal/test package variants
	var loadedTypes bool
	// ifaceMethodRefs collects interface methods that are used: a call
	// through an interface (iface.M()) resolves to the INTERFACE's M, not
	// the concrete implementation, so we must later credit every concrete
	// type implementing that interface (else its M reads as dead — the
	// dominant false positive on interface-heavy code).
	type ifaceMethodRef struct {
		iface  *types.Interface
		method string
	}
	var ifaceRefs []ifaceMethodRef
	var concreteTypes []*types.Named // every concrete (non-interface) named type loaded

	for _, p := range pkgs {
		if p.TypesInfo == nil || p.Types == nil {
			continue
		}
		loadedTypes = true
		// Definitions: every func/method declared in this package.
		for ident, obj := range p.TypesInfo.Defs {
			fn, ok := obj.(*types.Func)
			if !ok || fn.Pkg() == nil {
				continue
			}
			// init / main are runtime entry points — never explicitly
			// called, so never dead.
			if fn.Name() == "init" || fn.Name() == "main" {
				continue
			}
			pos := p.Fset.Position(ident.Pos())
			// Don't report test code as dead (TestXxx are `go test` entry
			// points; helpers in _test.go are test scaffolding). Matches the
			// name-based tool, which excludes test files. Their USES still
			// count, since the test package's TypesInfo.Uses is scanned.
			if strings.HasSuffix(pos.Filename, "_test.go") {
				continue
			}
			owner := ""
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				owner = recvTypeName(sig.Recv().Type())
			}
			id := symbolID(fn.Pkg().Path(), owner, fn.Name())
			if seenDef[id] {
				continue
			}
			seenDef[id] = true
			sym := Symbol{
				Pkg: fn.Pkg().Path(), Owner: owner, Name: fn.Name(),
				Path: pos.Filename, Line: pos.Line,
			}
			res.Defs = append(res.Defs, sym)
			res.defByID[id] = sym
		}
		// Concrete named types declared in this package (for interface
		// satisfaction below).
		if scope := p.Types.Scope(); scope != nil {
			for _, name := range scope.Names() {
				if tn, ok := scope.Lookup(name).(*types.TypeName); ok {
					if named, ok := tn.Type().(*types.Named); ok {
						if _, isIface := named.Underlying().(*types.Interface); !isIface {
							concreteTypes = append(concreteTypes, named)
						}
					}
				}
			}
		}
		// References: every use of a func/method (call, method value,
		// method expression). Interface methods are deferred to the
		// satisfaction pass; everything else is marked directly.
		for _, obj := range p.TypesInfo.Uses {
			fn, ok := obj.(*types.Func)
			if !ok {
				continue
			}
			// Mark the used func/method's own id referenced (covers direct
			// calls and interface-method declarations themselves).
			if id := funcID(fn); id != "" {
				res.referenced[id] = true
			}
			// If it's an interface method, also record it so the
			// satisfaction pass credits concrete implementers.
			if sig, ok := fn.Type().(*types.Signature); ok && sig.Recv() != nil {
				if iface, ok := sig.Recv().Type().Underlying().(*types.Interface); ok {
					ifaceRefs = append(ifaceRefs, ifaceMethodRef{iface: iface, method: fn.Name()})
				}
			}
		}
		// Call-graph edges: attribute each call site to its enclosing func
		// and resolved callee (powers who_calls / impact).
		res.collectEdges(p)
	}
	if !loadedTypes {
		return nil, false, nil
	}

	// Interface satisfaction: for every used interface method, credit the
	// matching method on every concrete type that implements the interface
	// (value or pointer receiver). Conservative — keeps a concrete method
	// alive whenever it COULD be dispatched through a used interface, which
	// is the safe direction for dead-code (avoid false positives).
	for _, ref := range ifaceRefs {
		for _, c := range concreteTypes {
			if c.Obj().Pkg() == nil {
				continue
			}
			if types.Implements(c, ref.iface) || types.Implements(types.NewPointer(c), ref.iface) {
				res.referenced[symbolID(c.Obj().Pkg().Path(), c.Obj().Name(), ref.method)] = true
			}
		}
	}
	return res, true, nil
}
