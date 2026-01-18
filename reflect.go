package jsonrpc

import (
	"errors"
	"reflect"
)

type funcValue reflect.Value

func newFuncValue(fn any) (funcValue, error) {
	v := reflect.ValueOf(fn)
	if v.Kind() != reflect.Func {
		return funcValue{}, errors.New("not a function")
	}
	t := v.Type()
	if t.NumIn() != 1 {
		return funcValue{}, errors.New("function must have 1 arg")
	}
	if t.NumOut() != 2 {
		return funcValue{}, errors.New("function must have 2 return types")
	}
	if !t.Out(1).Implements(reflect.TypeFor[error]()) {
		return funcValue{}, errors.New("second return type must be error")
	}
	return funcValue(v), nil
}

func (v funcValue) NewArgs() reflect.Value {
	t := reflect.Value(v).Type()
	argType := t.In(0)
	return reflect.New(argType)
}

func (fv funcValue) Call(v reflect.Value) (any, error) {
	out := reflect.Value(fv).Call([]reflect.Value{v})
	result := out[0]
	err := out[1]
	if !err.IsNil() {
		return nil, err.Interface().(error)
	}
	return result.Interface(), nil
}
