package ops

import "github.com/lab47/exprcore/exprcore"

func DecodeSum(sum exprcore.Value) (string, string, error) {
	switch v := sum.(type) {
	case exprcore.Tuple:
		var sumType, sumVal exprcore.String

		if len(v) != 2 {
			return "", "", ErrSumFormat
		}

		var ok bool

		sumType, ok = v[0].(exprcore.String)
		if !ok {
			return "", "", ErrSumFormat
		}

		sumVal, ok = v[1].(exprcore.String)
		if !ok {
			return "", "", ErrSumFormat
		}

		return string(sumType), string(sumVal), nil
	case exprcore.String:
		return "self", string(v), nil
	default:
		return "", "", ErrSumFormat
	}
}

func CompareEtag(a, b string) bool {
	if a[0] == '"' {
		a = a[1 : len(a)-2]
	}

	if b[0] == '"' {
		b = b[1 : len(b)-2]
	}

	return a == b
}
