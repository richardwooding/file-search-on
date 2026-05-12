package projecttype

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"
	"github.com/google/cel-go/common/types"
)

// dirCELEnv is the cel.Env every project-type CELExpr indicator
// compiles against. Two list<string> variables:
//
//	files    — basenames of files in the inspected directory
//	subdirs  — basenames of immediate subdirectories
//
// Standard CEL operators (in, exists, endsWith, startsWith, matches,
// size, …) cover the vocabulary for MVP; no custom functions yet.
//
// Built lazily once at first use, then reused — env construction is
// cheap but not free and cel-go's runtime caches well.
var (
	dirCELEnvOnce sync.Once
	dirCELEnv     *cel.Env
	dirCELEnvErr  error
)

func dirEnv() (*cel.Env, error) {
	dirCELEnvOnce.Do(func() {
		dirCELEnv, dirCELEnvErr = cel.NewEnv(
			cel.Variable("files", cel.ListType(cel.StringType)),
			cel.Variable("subdirs", cel.ListType(cel.StringType)),
		)
	})
	return dirCELEnv, dirCELEnvErr
}

// compileDirCEL parses and type-checks expr against dirEnv. Returns
// a Program ready for Eval. Called from compileIndicators at
// Register-time.
func compileDirCEL(expr string) (cel.Program, error) {
	env, err := dirEnv()
	if err != nil {
		return nil, fmt.Errorf("project-type CEL env: %w", err)
	}
	ast, issues := env.Compile(expr)
	if issues != nil && issues.Err() != nil {
		return nil, fmt.Errorf("compile %q: %w", expr, issues.Err())
	}
	prog, err := env.Program(ast)
	if err != nil {
		return nil, fmt.Errorf("program %q: %w", expr, err)
	}
	return prog, nil
}

// evalDirCEL runs prog against (files, subdirs) and returns true iff
// it resolves to a CEL boolean true. Any error (type mismatch,
// runtime issue) is swallowed as false — an indicator that can't
// evaluate cleanly shouldn't fire.
func evalDirCEL(prog cel.Program, files, subdirs []string) bool {
	out, _, err := prog.Eval(map[string]any{
		"files":   files,
		"subdirs": subdirs,
	})
	if err != nil {
		return false
	}
	return out == types.True
}
