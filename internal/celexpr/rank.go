package celexpr

import (
	"fmt"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
)

// RankEvaluator evaluates a CEL expression returning a numeric score
// against a *FileAttributes. Used by the walker's --rank path
// (issue #168) so callers can specify custom sort keys like
// `similarity * 0.7 + recency_bonus`.
//
// Shares the *cel.Env of an existing Evaluator so the same CEL
// vocabulary works on both filter + rank sides. Compile a fresh
// RankEvaluator with (*Evaluator).NewRank(expr).
type RankEvaluator struct {
	prog cel.Program
}

// NewRank compiles `expr` as a CEL expression against the receiver's
// environment. The expression may return a double, an int, or a
// bool — `Eval` coerces all three into a float64 score.
//
// Lists / maps / strings / timestamps as the top-level type are
// not supported and produce a clear error at the first Eval call
// (cel-go resolves return types lazily at evaluation time, not at
// compile time).
func (e *Evaluator) NewRank(expr string) (*RankEvaluator, error) {
	ast, issues := e.env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compiling rank expression: %w", issues.Err())
	}
	prog, err := e.env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("creating rank program: %w", err)
	}
	return &RankEvaluator{prog: prog}, nil
}

// Eval evaluates the rank expression against the given attributes
// and coerces the return value into a float64. Supports:
//
//   - types.Double → underlying float64
//   - types.Int → coerced to float64
//   - types.Bool → 1.0 for true, 0.0 for false (lets shortcuts like
//     `--rank 'is_pdf'` surface PDFs first without ternary scaffolding)
//
// Other return types (list, map, string, timestamp, etc.) produce
// a clear error. The walker treats this error as "rank zero for
// this file" rather than dropping the file from results — partial
// data is better than missing matches.
func (r *RankEvaluator) Eval(attrs *FileAttributes) (float64, error) {
	out, _, err := r.prog.Eval(&fileAttrsActivation{attrs: attrs})
	if err != nil {
		return 0, fmt.Errorf("evaluating rank expression: %w", err)
	}
	return coerceRankValue(out)
}

func coerceRankValue(v ref.Val) (float64, error) {
	switch t := v.(type) {
	case types.Double:
		return float64(t), nil
	case types.Int:
		return float64(t), nil
	case types.Bool:
		if bool(t) {
			return 1.0, nil
		}
		return 0.0, nil
	}
	return 0, fmt.Errorf("rank expression returned %T; want double, int, or bool", v)
}
