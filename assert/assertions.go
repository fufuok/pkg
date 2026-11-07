package assert

import (
	"bytes"
	"fmt"
	"log"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"text/tabwriter"
)

func True(tb testing.TB, value bool, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if value {
		return
	}
	result := "Should be true"
	assertLog(tb, nil, value, "True", result, msgAndArgs...)
}

func False(tb testing.TB, value bool, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if !value {
		return
	}
	result := "Should be false"
	assertLog(tb, nil, value, "False", result, msgAndArgs...)
}

func NotNil(tb testing.TB, value any, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if !IsNil(value) {
		return
	}
	result := "Expected value not to be nil."
	assertLog(tb, nil, value, "NotNil", result, msgAndArgs...)
}

func Nil(tb testing.TB, value any, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if IsNil(value) {
		return
	}
	result := fmt.Sprintf("Expected nil, but got: %#v", value)
	assertLog(tb, nil, value, "Nil", result, msgAndArgs...)
}

func NotEmpty(tb testing.TB, value any, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if !IsEmpty(value) {
		return
	}
	result := fmt.Sprintf("Should NOT be empty, but was %v", value)
	assertLog(tb, nil, value, "NotEmpty", result, msgAndArgs...)
}

func Empty(tb testing.TB, value any, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if IsEmpty(value) {
		return
	}
	result := fmt.Sprintf("Should be empty, but was %v", value)
	assertLog(tb, nil, value, "Empty", result, msgAndArgs...)
}

func Contains(tb testing.TB, value string, msgAndArgs ...string) {
	if tb != nil {
		tb.Helper()
	}
	for _, s := range msgAndArgs {
		if ok := strings.Contains(s, value); !ok {
			result := fmt.Sprintf("%s does not contain %s", s, value)
			assertLog(tb, nil, nil, "Contains", result, msgAndArgs)
		}
	}
}

func NotEqual(tb testing.TB, expected, actual any, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if !DeepEqual(expected, actual) {
		return
	}
	result := fmt.Sprintf("Should not be: %#v\n", actual)
	assertLog(tb, expected, actual, "NotEqual", result, msgAndArgs...)
}

// Equal checks if values are equal
// Ref: gofiber/utils
func Equal(tb testing.TB, expected, actual any, msgAndArgs ...any) {
	if tb != nil {
		tb.Helper()
	}
	if DeepEqual(expected, actual) {
		return
	}
	assertLog(tb, expected, actual, "Equal", "", msgAndArgs...)
}

func assertLog(tb testing.TB, a, b any, testType, result string, msgAndArgs ...any) {
	if tb != nil {
		testType = fmt.Sprintf("%s(%s)", tb.Name(), testType)
	}

	_, file, line, _ := runtime.Caller(2)

	var buf bytes.Buffer
	w := tabwriter.NewWriter(&buf, 0, 0, 5, ' ', 0)
	_, _ = fmt.Fprintf(w, "\nTest:\t%s", testType)
	_, _ = fmt.Fprintf(w, "\nTrace:\t%s:%d", filepath.Base(file), line)
	message := messageFromMsgAndArgs(msgAndArgs...)
	if message != "" {
		_, _ = fmt.Fprintf(w, "\nDescription:\t%s", message)
	}
	if result != "" {
		_, _ = fmt.Fprintf(w, "\nResult:\t%s", result)
	} else {
		aType := "<nil>"
		bType := "<nil>"
		if a != nil {
			aType = reflect.TypeOf(a).String()
		}
		if b != nil {
			bType = reflect.TypeOf(b).String()
		}
		_, _ = fmt.Fprintf(w, "\nResult:\tNot equal")
		_, _ = fmt.Fprintf(w, "\nExpected:\t%v\t(%s)", a, aType)
		_, _ = fmt.Fprintf(w, "\nActual:\t%v\t(%s)", b, bType)
	}

	msg := ""
	if err := w.Flush(); err != nil {
		msg = err.Error()
	} else {
		msg = buf.String()
	}

	if tb != nil {
		tb.Fatal(msg)
	} else {
		log.Fatal(msg)
	}
}

// Ref: stretchr/testify
func messageFromMsgAndArgs(msgAndArgs ...any) string {
	if len(msgAndArgs) == 0 || msgAndArgs == nil {
		return ""
	}
	if len(msgAndArgs) == 1 {
		msg := msgAndArgs[0]
		if msgAsStr, ok := msg.(string); ok {
			return runeSubString(msgAsStr, 300, "..")
		}
		return runeSubString(fmt.Sprintf("%+v", msg), 300, "..")
	}
	if len(msgAndArgs) > 1 {
		return runeSubString(fmt.Sprintf(msgAndArgs[0].(string), msgAndArgs[1:]...), 300, "..")
	}
	return ""
}

// Panics 断言 panic
func Panics(t *testing.T, title string, f func()) {
	defer func() {
		if r := recover(); r == nil {
			if t != nil {
				t.Fatalf("%s: didn't panic as expected", title)
			} else {
				log.Fatalf("%s: didn't panic as expected", title)
			}
		}
	}()
	f()
}

// DeepEqual Ref: stretchr/testify
func DeepEqual(expected, actual any) bool {
	if expected == nil || actual == nil {
		return expected == actual
	}

	exp, ok := expected.([]byte)
	if !ok {
		return reflect.DeepEqual(expected, actual)
	}

	act, ok := actual.([]byte)
	if !ok {
		return false
	}
	if exp == nil || act == nil {
		return exp == nil && act == nil
	}
	return bytes.Equal(exp, act)
}

// IsNil 判断 nil 接口, 以及接口中保存的 channel, func, interface, map, pointer, slice 或 unsafe.Pointer 是否为 nil.
// 接口只有在动态类型和值均为空时才等于 nil; 保存带类型的 nil 指针时, 接口本身不等于 nil,
// 本函数仍会通过反射将该值识别为 nil. struct, int 等不可为 nil 的类型返回 false.
//
// Ref: stretchr/testify
func IsNil(o any) bool {
	if o == nil {
		return true
	}

	value := reflect.ValueOf(o)
	kind := value.Kind()
	isNilableKind := containsKind(
		[]reflect.Kind{
			reflect.Chan, reflect.Func,
			reflect.Interface, reflect.Map,
			reflect.Pointer, reflect.Slice, reflect.UnsafePointer,
		},
		kind,
	)

	if isNilableKind && value.IsNil() {
		return true
	}

	return false
}

// containsKind checks if a specified kind in the slice of kinds.
// Ref: stretchr/testify
func containsKind(kinds []reflect.Kind, kind reflect.Kind) bool {
	return slices.Contains(kinds, kind)
}

// IsEmpty gets whether the specified object is considered empty or not.
// Ref: stretchr/testify
func IsEmpty(o any) bool {
	// get nil case out of the way
	if o == nil {
		return true
	}

	v := reflect.ValueOf(o)
	switch v.Kind() {
	// collection types are empty when they have no element
	case reflect.Chan, reflect.Map, reflect.Slice:
		return v.Len() == 0
	// pointers are empty if nil or if the value they point to is empty
	case reflect.Pointer:
		if v.IsNil() {
			return true
		}
		deref := v.Elem().Interface()
		return IsEmpty(deref)
	// for all other types, compare against the zero value
	// array types are empty when they match their zero-initialized state
	default:
		zero := reflect.Zero(v.Type())
		return reflect.DeepEqual(o, zero.Interface())
	}
}
