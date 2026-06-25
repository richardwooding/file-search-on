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
type tsCognitiveSpec struct {
	// nesting nodes cost 1 + nesting and raise the nesting level for children.
	nesting map[string]bool
	// flat nodes cost a flat 1 (continuations like elif_clause / else_clause).
	flat map[string]bool
	// elseField, when set, marks a nesting node reached as the named field of a
	// same-type parent as an `else if` (flat cost, no nesting penalty) — e.g.
	// JS/TS/Java if_statement in a parent if_statement's "alternative".
	elseField string
	// elseParentType, when set, marks a nesting node whose direct parent is this
	// type as an `else if` — e.g. Rust if_expression under an else_clause.
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
		nesting:   tsNodeSet("if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement", "switch_statement", "catch_clause", "ternary_expression"),
		flat:      tsNodeSet(),
		elseField: "alternative",
	},
	"typescript": {
		nesting:   tsNodeSet("if_statement", "for_statement", "for_in_statement", "while_statement", "do_statement", "switch_statement", "catch_clause", "ternary_expression"),
		flat:      tsNodeSet(),
		elseField: "alternative",
	},
	"java": {
		nesting:   tsNodeSet("if_statement", "for_statement", "enhanced_for_statement", "while_statement", "do_statement", "switch_expression", "catch_clause", "ternary_expression"),
		flat:      tsNodeSet(),
		elseField: "alternative",
	},
	"rust": {
		nesting:        tsNodeSet("if_expression", "while_expression", "for_expression", "loop_expression", "match_expression"),
		flat:           tsNodeSet(),
		elseParentType: "else_clause",
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

	var walk func(n *ts.Node, spanIdx, nesting int)
	walk = func(n *ts.Node, spanIdx, nesting int) {
		for i := 0; i < n.ChildCount(); i++ {
			c := n.Child(i)
			if c == nil {
				continue
			}
			// Entering a function definition starts a fresh nesting context so
			// depth is measured per-function (and matches the per-function rows).
			if idx, ok := byRange[[2]uint32{c.StartByte(), c.EndByte()}]; ok {
				walk(c, idx, 0)
				continue
			}
			t := c.Type(tl.lang)
			switch {
			case spec.nesting[t]:
				if tsIsElseContinuation(c, t, spec, tl.lang) {
					if spanIdx >= 0 {
						cog[spanIdx]++ // else if: flat cost, no nesting penalty
					}
					walk(c, spanIdx, nesting)
				} else {
					if spanIdx >= 0 {
						cog[spanIdx] += 1 + nesting
					}
					walk(c, spanIdx, nesting+1)
				}
			case spec.flat[t]:
				if spanIdx >= 0 {
					cog[spanIdx]++
				}
				walk(c, spanIdx, nesting)
			case tsBoolOp(c, tl.lang) != "":
				if spanIdx >= 0 && !tsSameRunAsParent(c, tl.lang) {
					cog[spanIdx]++
				}
				walk(c, spanIdx, nesting)
			default:
				walk(c, spanIdx, nesting)
			}
		}
	}
	walk(tree.RootNode(), -1, 0)

	out := make([]*int, len(funcSpans))
	for i := range cog {
		v := cog[i]
		out[i] = &v
	}
	return out
}

// tsIsElseContinuation reports whether a nesting node is actually an `else if`
// — a nested if in the else branch — which costs a flat 1 with no nesting
// penalty (the SonarSource rule the Go path also applies).
func tsIsElseContinuation(n *ts.Node, t string, spec tsCognitiveSpec, lang *ts.Language) bool {
	parent := n.Parent()
	if parent == nil {
		return false
	}
	if spec.elseField != "" && parent.Type(lang) == t {
		if alt := parent.ChildByFieldName(spec.elseField, lang); alt != nil && tsSameNode(alt, n) {
			return true
		}
	}
	if spec.elseParentType != "" && parent.Type(lang) == spec.elseParentType {
		return true
	}
	return false
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
// node with the same operator — i.e. n continues an existing run (a && b && c
// is one run) and so must not be counted again.
func tsSameRunAsParent(n *ts.Node, lang *ts.Language) bool {
	op := tsBoolOp(n, lang)
	if op == "" {
		return false
	}
	parent := n.Parent()
	return parent != nil && tsBoolOp(parent, lang) == op
}

// tsSameNode compares two nodes by type + byte span (gotreesitter may hand back
// distinct *Node values for the same syntactic node, so pointer equality is
// unreliable).
func tsSameNode(a, b *ts.Node) bool {
	return a.StartByte() == b.StartByte() && a.EndByte() == b.EndByte()
}
