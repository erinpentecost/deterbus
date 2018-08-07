package deterbus

import (
	"fmt"
	"reflect"
)

func getInputTypes(fnType reflect.Type) ([]reflect.Type, error) {
	if fnType.Kind() != reflect.Func {
		return nil, fmt.Errorf("%s is not of type reflect.Func", fnType.Kind())
	}

	count := reflect.Type.NumIn(fnType)
	foundTypes := make([]reflect.Type, count)

	for i := 0; i < count; i++ {
		foundTypes[i] = reflect.Type.In(fnType, i)
	}

	return foundTypes, nil
}

func getTypes(args ...interface{}) []reflect.Type {
	foundTypes := make([]reflect.Type, len(args))

	for i := 0; i < len(args); i++ {
		foundTypes[i] = reflect.TypeOf(args[i])
	}

	return foundTypes
}

func typesMatch(a []reflect.Type, b []reflect.Type) (bool, error) {
	if len(a) != len(b) {
		return false, fmt.Errorf("differing lengths: %v and %v", len(a), len(b))
	}

	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false, fmt.Errorf("a[%v] != b[%v], %s != %s", i, i, a[i].Name(), b[i].Name())
		}
	}
	return true, nil
}
