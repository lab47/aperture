package lang

import (
	"errors"

	"github.com/lab47/exprcore/exprcore"
)

var (
	ErrNotString = errors.New("value not a string")
	ErrNotFunc   = errors.New("value not a function")
	ErrNotList   = errors.New("value not a list")
)

func StringValue(v exprcore.Value, err error) (string, error) {
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			return "", nil
		}
		return "", err
	}

	if v == nil {
		return "", nil
	}

	str, ok := v.(exprcore.String)
	if !ok {
		return "", ErrNotString
	}

	return string(str), nil
}

func FuncValue(v exprcore.Value, err error) (*exprcore.Function, error) {
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			return nil, nil
		}
		return nil, err
	}

	if v == nil {
		return nil, nil
	}

	fn, ok := v.(*exprcore.Function)
	if !ok {
		return nil, ErrNotFunc
	}

	return fn, nil
}

func ListValue(v exprcore.Value, err error) (*exprcore.List, error) {
	if err != nil {
		if _, ok := err.(exprcore.NoSuchAttrError); ok {
			return nil, nil
		}
		return nil, err
	}

	if v == nil {
		return nil, nil
	}

	list, ok := v.(*exprcore.List)
	if !ok {
		return nil, ErrNotList
	}

	return list, nil
}
