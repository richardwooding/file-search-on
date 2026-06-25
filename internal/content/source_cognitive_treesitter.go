package content

import (
	ts "github.com/odvcencio/gotreesitter"
)

// Cognitive complexity for the tree-sitter languages (issue #491), the
// counterpart to the precise go/ast implementation in source_cognitive_go.go.
// Unlike the flat decision-point count used for cyclomatic complexity, this
// walks each function's parse subtree tracking nesting depth and applies the
// SonarSource increments: a structural node (if / loop / switch / catch /
// ternary) costs 1 + the current nesting and raises the nesting for its body;
// a continuation (else / else-if) costs a flat 1; and each maximal run of like
// logical operators costs 1.
//
// It is enabled per-language via tsCognitiveSpecs. Languages without an entry
// return no rows, so complexity reports their cognitive value as unavailable
// (the *int stays nil) rather than a wrong number. The C-family grammars model
// `else if` as a nested if in the else branch; tsCognitiveSpec.elseField /
// elseParentType let the walk recognise that shape and charge it the flat
// else-if cost, matching the precise Go behaviour.

// tsCognitiveSpec classifies one grammar's nodes for the cognitive walk.
//
// `else if` detection is parent-driven: when the walk is AT an if node (ifType)
// it tags its else-branch if-child's byte range, and a tagged node is later
// charged the flat else-if cost. This avoids Node.Parent(), which gotreesitter
// routes through hidden supertype wrappers (e.g. C#'s `statement`) and which
// conflates Kotlin's meaningful `statements` wrapper.
type tsCognitiveSpec struct {
	// nesting nodes cost 1 + nesting and raise the nesting level for children.
	nesting map[string]bool
	// flat nodes cost a flat 1 (continuations like elif_clause / else_clause).
	flat map[string]bool
	// ifType is the grammar's if node — the only node whose else branch is
	// scanned for an else-if. Empty disables else-if detection (the grammar's
	// else/elif are then distinct flat nodes, e.g. Python / PHP).
	ifType string
	// elseField, when set, is the field on an if node holding the else branch;
	// when that field's value is itself an ifType node, it's an `else if`
	// (JS/TS/Java/C# "alternative").
	elseField string
	// elseParentType, when set, is the wrapper node holding the else branch
	// (Rust/C/C++ "else_clause", Kotlin "control_structure_body"); a direct
	// ifType child of that wrapper is an `else if`.
	elseParentType string
}

func tsNodeSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// tsCognitiveSpecs is the per-language enablement + node classification. Adding
// a language is a new entry plus reference tests (#491 tracks the long tail).
var tsCognitiveSpecs = map[string]tsCognitiveSpec{
	"python": {
		nesting: tsNodeSet("if_statement", "for_statement", "while_statement", "except_clause", "conditional_expression", "match_statement"),
		flat:    tsNodeSet("elif_clause", "else_clause"),
	},
	"javascript": {
		nesting:   tsNodeSet("if_statement", "for_statement", "for_in_statement", "for_of_statement", "while_statement", "do_statement", "switch_statement", "catch_clause", "ternary_expression"),
		ifType:    "if_statement",
		elseField: "alternative",
	},
	"typescript": {
		nesting:   tsNodeSet("if_statement", "for_statement", "for_in_statement", "for_of_statement", "while_statement", "do_statement", "switch_statement", "catch_clause", "ternary_expression"),
		ifType:    "if_statement",
		elseField: "alternative",
	},
	"java": {
		nesting:   tsNodeSet("if_statement", "for_statement", "enhanced_for_statement", "while_statement", "do_statement", "switch_statement", "switch_expression", "catch_clause", "ternary_expression"),
		ifType:    "if_statement",
		elseField: "alternative",
	},
	"rust": {
		nesting:        tsNodeSet("if_expression", "while_expression", "for_expression", "loop_expression", "match_expression"),
		ifType:         "if_expression",
		elseParentType: "else_clause",
	},
	"c": {
		nesting:        tsNodeSet("if_statement", "for_statement", "while_statement", "do_statement", "switch_statement", "conditional_expression"),
		ifType:         "if_statement",
		elseParentType: "else_clause",
	},
	"cpp": {
		nesting:        tsNodeSet("if_statement", "for_statement", "for_range_loop", "while_statement", "do_statement", "switch_statement", "catch_clause", "conditional_expression"),
		ifType:         "if_statement",
		elseParentType: "else_clause",
	},
	"csharp": {
		nesting:   tsNodeSet("if_statement", "for_statement", "foreach_statement", "while_statement", "do_statement", "switch_statement", "catch_clause", "conditional_expression"),
		ifType:    "if_statement",
		elseField: "alternative",
	},
	"kotlin": {
		// else-if is an if_expression directly under the else control_structure_body;
		// a braced body wraps its contents in a `statements` node instead, so a
		// direct if_expression child only appears for a genuine else-if (a
		// braceless `if a if b` then-body is the rare exception).
		nesting:        tsNodeSet("if_expression", "for_statement", "while_statement", "do_while_statement", "when_expression", "catch_block"),
		ifType:         "if_expression",
		elseParentType: "control_structure_body",
	},
	"php": {
		nesting: tsNodeSet("if_statement", "for_statement", "foreach_statement", "while_statement", "do_statement", "switch_statement", "catch_clause", "conditional_expression"),
		flat:    tsNodeSet("else_if_clause", "else_clause"),
	},
	"ruby": {
		// `else` is shared by if and case, so it is NOT flat (that would
		// over-count a case/when else); only the if-specific `elsif` is. A
		// trailing plain else then under-counts by 1 (as in the C-family).
		// `case` is the switch container (its `when`s are free); `conditional`
		// is the ternary.
		nesting: tsNodeSet("if", "unless", "while", "until", "for", "case", "rescue", "conditional"),
		flat:    tsNodeSet("elsif"),
	},
	"scala": {
		// match_expression is the container (case_clauses free). Booleans are
		// infix_expression with an operator_identifier child, which tsBoolOp
		// doesn't read, so && / || runs aren't counted for Scala (under-count).
		nesting:   tsNodeSet("if_expression", "for_expression", "while_expression", "match_expression", "catch_clause"),
		ifType:    "if_expression",
		elseField: "alternative",
	},
	"r": {
		nesting:   tsNodeSet("if_statement", "for_statement", "while_statement"),
		ifType:    "if_statement",
		elseField: "alternative",
	},
	"matlab": {
		nesting: tsNodeSet("if_statement", "for_statement", "while_statement", "switch_statement"),
		flat:    tsNodeSet("elseif_clause", "else_clause"),
	},
	"perl": {
		// if = conditional_statement, while/for = loop_statement; elsif is a
		// distinct flat node.
		nesting: tsNodeSet("conditional_statement", "loop_statement"),
		flat:    tsNodeSet("elsif"),
	},
}

// tsCognitiveComplexity computes cognitive complexity for each function span,
// or returns nil when the language has no spec (cognitive unavailable). The
// result is index-aligned with funcSpans; each entry is a fresh *int.
func tsCognitiveComplexity(language string, tl *tsLang, tree *ts.Tree, funcSpans []tsFuncSpan) []*int {
	spec, ok := tsCognitiveSpecs[language]
	if !ok {
		return nil // language not enabled → cognitive unavailable
	}
	if tree == nil || len(funcSpans) == 0 {
		return []*int{}
	}
	cog := make([]int, len(funcSpans))
	byRange := make(map[[2]uint32]int, len(funcSpans))
	for i, s := range funcSpans {
		byRange[[2]uint32{s.start, s.end}] = i
	}
	// elseIf holds the byte ranges of if-nodes that sit in an else branch (an
	// `else if`), tagged from the parent if while walking — see tsTagElseIf.
	elseIf := map[[2]uint32]bool{}

	var walk func(n *ts.Node, spanIdx, nesting int)
	walk = func(n *ts.Node, spanIdx, nesting int) {
		for i := 0; i < n.ChildCount(); i++ {
			c := n.Child(i)
			if c == nil {
				continue
			}
			// Skip anonymous tokens (keywords, punctuation): they're leaves and
			// some grammars name a keyword the same as its statement — Ruby's
			// `if` token shares the type string with the `if` node — which would
			// double-count. Named nodes carry all the structure we classify.
			if !c.IsNamed() {
				continue
			}
			rng := [2]uint32{c.StartByte(), c.EndByte()}
			// Entering a function definition starts a fresh nesting context so
			// depth is measured per-function (and matches the per-function rows).
			if idx, ok := byRange[rng]; ok {
				walk(c, idx, 0)
				continue
			}
			// if-else (not switch) so the logical-operator branch computes the
			// operator once and threads it into tsSameRunAsParent — tsBoolOp
			// walks children + makes CGO Type calls, so it's worth not repeating.
			t := c.Type(tl.lang)
			if spec.nesting[t] {
				if elseIf[rng] {
					if spanIdx >= 0 {
						cog[spanIdx]++ // else if: flat cost, no nesting penalty
					}
					// Tag this else-if's own else branch too, so a chain
					// (else if … else if …) stays flat the whole way down.
					tsTagElseIf(c, t, spec, tl.lang, elseIf)
					walk(c, spanIdx, nesting)
				} else {
					if spanIdx >= 0 {
						cog[spanIdx] += 1 + nesting
					}
					tsTagElseIf(c, t, spec, tl.lang, elseIf)
					walk(c, spanIdx, nesting+1)
				}
			} else if spec.flat[t] {
				if spanIdx >= 0 {
					cog[spanIdx]++
				}
				walk(c, spanIdx, nesting)
			} else if op := tsBoolOp(c, tl.lang); op != "" {
				if spanIdx >= 0 && !tsSameRunAsParent(c, op, tl.lang) {
					cog[spanIdx]++
				}
				walk(c, spanIdx, nesting)
			} else {
				walk(c, spanIdx, nesting)
			}
		}
	}
	walk(tree.RootNode(), -1, 0)

	// Point into the single cog backing array — one allocation, vs a per-span
	// heap escape from taking &(loop-local).
	out := make([]*int, len(cog))
	for i := range cog {
		out[i] = &cog[i]
	}
	return out
}

// tsTagElseIf records, while the walk is AT an if node (c, type t), the byte
// range of an if-node sitting in its else branch — an `else if`, which should
// be charged the flat else-if cost rather than a nested-if cost. Driven from
// the parent (via the else field / wrapper child) so it never relies on
// Node.Parent(), which gotreesitter routes through hidden wrappers.
func tsTagElseIf(c *ts.Node, t string, spec tsCognitiveSpec, lang *ts.Language, set map[[2]uint32]bool) {
	if spec.ifType == "" || t != spec.ifType {
		return
	}
	if spec.elseField != "" {
		// The else branch is a field whose value, when an ifType node, is an else if.
		if alt := c.ChildByFieldName(spec.elseField, lang); alt != nil && alt.Type(lang) == t {
			set[[2]uint32{alt.StartByte(), alt.EndByte()}] = true
		}
	}
	if spec.elseParentType != "" {
		// The else branch is a wrapper child; a direct ifType child of it is an else if.
		for i := 0; i < c.ChildCount(); i++ {
			w := c.Child(i)
			if w == nil || w.Type(lang) != spec.elseParentType {
				continue
			}
			for j := 0; j < w.ChildCount(); j++ {
				if gc := w.Child(j); gc != nil && gc.Type(lang) == t {
					set[[2]uint32{gc.StartByte(), gc.EndByte()}] = true
				}
			}
		}
	}
}

// tsBoolOp returns the logical operator a node represents ("&&" / "||"), or ""
// when it isn't a logical-operator node. Handles the binary-expression form
// (operator as an anonymous token child, incl. Python's and/or), and the
// distinct conjunction/disjunction node types (Kotlin / Swift).
func tsBoolOp(n *ts.Node, lang *ts.Language) string {
	switch n.Type(lang) {
	case "conjunction_expression":
		return "&&"
	case "disjunction_expression":
		return "||"
	}
	for i := 0; i < n.ChildCount(); i++ {
		ch := n.Child(i)
		if ch == nil || ch.IsNamed() { // the operator is an anonymous token
			continue
		}
		switch ch.Type(lang) {
		case "&&", "and":
			return "&&"
		case "||", "or":
			return "||"
		}
	}
	return ""
}

// tsSameRunAsParent reports whether n's immediate parent is a logical-operator
// node with the same operator op — i.e. n continues an existing run (a && b &&
// c is one run) and so must not be counted again. op is the caller's already-
// computed tsBoolOp(n) (non-empty).
func tsSameRunAsParent(n *ts.Node, op string, lang *ts.Language) bool {
	parent := n.Parent()
	return parent != nil && tsBoolOp(parent, lang) == op
}
