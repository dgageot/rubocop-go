package cops

import (
	"go/ast"
	"go/types"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/dgageot/rubocop-go/cop"
)

// NewLintWrapErrors flags fmt.Errorf calls that interpolate an error value without
// the %w verb.
//
// Formatting an error with %v or %s discards its chain, preventing callers
// from inspecting the cause with errors.Is and errors.As.
//
// The rule only fires when an argument's type is exactly the built-in error
// interface, so passing a string field named Error (a common shape on API
// response types) is not mistaken for an error value. A format string that
// already contains %w is left alone — wrapping one error per call is enough
// to keep the chain intact.
//
// Per-line suppression: `//rubocop:disable Lint/WrapErrors`.
func NewLintWrapErrors(opts ...cop.FuncOption) *cop.Func {
	return configuredFileCop(&cop.Func{
		Meta: cop.Meta{
			Name:        "Lint/WrapErrors",
			Description: "Preserve error chains when formatting errors.",
			Severity:    cop.Warning,
		},
		Types: true,
		Run: func(p *cop.Pass) {
			if p.Info == nil {
				return
			}
			p.ForEachCall(func(call *ast.CallExpr) {
				fn, ok := p.CalleeObject(call).(*types.Func)
				if !ok || fn.Pkg() == nil || fn.Pkg().Path() != "fmt" || fn.Name() != "Errorf" || len(call.Args) < 2 || call.Ellipsis.IsValid() {
					return
				}
				format, ok := p.StringValue(call.Args[0])
				if ok && formatInterpolatesError(p.Info, format, call.Args[1:]) {
					p.Report(call, "fmt.Errorf interpolates an error without %w — wrap it so errors.Is/As keep working")
				}
			})
		},
	}, opts...)
}

// formatInterpolatesError tracks operands separately from width and precision.
// Percent verbs consume no operand, and %T and %p do not render error text.
// Malformed directives are left to format-string diagnostics.
func formatInterpolatesError(info *types.Info, format string, args []ast.Expr) bool {
	arg, interpolates := 0, false
	for i := 0; i < len(format); {
		if format[i] != '%' {
			i++
			continue
		}
		i++
		for i < len(format) && strings.ContainsRune("#0+- ", rune(format[i])) {
			i++
		}
		var indexed, ok bool
		valid := true
		arg, i, indexed, ok = formatArgIndex(format, i, arg, len(args))
		if !ok {
			return false
		}
		if i < len(format) && format[i] == '*' {
			arg++
			i++
			indexed = false
		} else {
			start := i
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				i++
			}
			if indexed && i != start {
				valid = false // Only the percent verb accepts an index before a width.
			}
		}
		if i+1 < len(format) && format[i] == '.' {
			if indexed {
				valid = false
			}
			i++
			arg, i, indexed, ok = formatArgIndex(format, i, arg, len(args))
			if !ok {
				return false
			}
			if i < len(format) && format[i] == '*' {
				arg++
				i++
				indexed = false
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					i++
				}
			}
		}
		if !indexed {
			arg, i, _, ok = formatArgIndex(format, i, arg, len(args))
			if !ok {
				return false
			}
		}
		if i >= len(format) {
			return false
		}
		verb, size := utf8.DecodeRuneInString(format[i:])
		i += size
		if verb == '%' {
			continue
		}
		if !valid || arg >= len(args) {
			return false
		}
		if verb == 'w' {
			return false // Preserve the policy of allowing one wrapped error per call.
		}
		if verb != 'T' && verb != 'p' && isErrorType(info.TypeOf(args[arg])) {
			interpolates = true
		}
		arg++
	}
	return interpolates
}

func formatArgIndex(format string, i, arg, count int) (int, int, bool, bool) {
	if i >= len(format) || format[i] != '[' {
		return arg, i, false, true
	}
	end := strings.IndexByte(format[i:], ']')
	if end < 0 {
		return arg, i, false, false
	}
	digits := format[i+1 : i+end]
	for _, digit := range digits {
		if digit < '0' || digit > '9' {
			return arg, i, false, false
		}
	}
	index, err := strconv.Atoi(digits)
	if err != nil || index < 1 || index > count {
		return arg, i, false, false
	}
	return index - 1, i + end + 1, true, true
}

// isErrorType reports whether t is exactly the built-in error interface.
func isErrorType(t types.Type) bool {
	if t == nil {
		return false
	}
	named, ok := types.Unalias(t).(*types.Named)
	if !ok {
		return false
	}
	return named.Obj() != nil && named.Obj().Name() == "error" && named.Obj().Pkg() == nil
}
