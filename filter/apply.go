// Copyright 2014 The rspace Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package filter contains utility functions for filtering slices through the
// distributed application of a filter function
package filter

import (
	"reflect"
)

// Apply takes a slice of type []T and a function of type func(T) T. (If the
// input conditions are not satisfied, Apply panics.) It returns a newly
// allocated slice where each element is the result of calling the function on
// successive elements of the slice.
func Apply(slice, function interface{}) interface{} {
	return apply(slice, function, false)
}

// ApplyInPlace is like Apply, but overwrites the slice rather than returning a
// newly allocated slice.
func ApplyInPlace(slice, function interface{}) {
	apply(slice, function, true)
}

// Choose takes a slice of type []T and a function of type func(T) bool. (If
// the input conditions are not satisfied, Choose panics.) It returns a newly
// allocated slice containing only those elements of the input slice that
// satisfy the function.
func Choose(slice, function interface{}) interface{} {
	out, _ := chooseOrDrop(slice, function, false, true)
	return out
}

// Drop takes a slice of type []T and a function of type func(T) bool. (If the
// input conditions are not satisfied, Drop panics.) It returns a newly
// allocated slice containing only those elements of the input slice that do
// not satisfy the function, that is, it removes elements that satisfy the
// function.
func Drop(slice, function interface{}) interface{} {
	out, _ := chooseOrDrop(slice, function, false, false)
	return out
}

// ChooseInPlace is like Choose, but overwrites the slice rather than returning
// a newly allocated slice. Since ChooseInPlace must modify the header of the
// slice to set the new length, it takes as argument a pointer to a slice
// rather than a slice.
func ChooseInPlace(pointerToSlice, function interface{}) {
	chooseOrDropInPlace(pointerToSlice, function, true)
}

// DropInPlace is like Drop, but overwrites the slice rather than returning a
// newly allocated slice. Since DropInPlace must modify the header of the slice
// to set the new length, it takes as argument a pointer to a slice rather than
// a slice.
func DropInPlace(pointerToSlice, function interface{}) {
	chooseOrDropInPlace(pointerToSlice, function, false)
}

func apply(slice, function interface{}, inPlace bool) interface{} {
	// Special case for strings, very common.
	if strSlice, ok := slice.([]string); ok {
		if strFn, ok := function.(func(string) string); ok {
			r := strSlice
			if !inPlace {
				r = make([]string, len(strSlice))
			}
			for i, s := range strSlice {
				r[i] = strFn(s)
			}
			return r
		}
	}
	in := reflect.ValueOf(slice)
	if in.Kind() != reflect.Slice {
		panic("apply: not slice")
	}
	fn := reflect.ValueOf(function)
	if fn.Kind() != reflect.Func {
		panic("apply: not function")
	}
	if fn.Type().NumIn() != 1 || fn.Type().NumOut() != 1 || fn.Type().In(0) != in.Type().Elem() {
		panic("apply: function must be of type func(" + in.Type().Elem().String() + ")  outputElemType")
	}
	out := in
	if !inPlace {
		out = reflect.MakeSlice(reflect.SliceOf(fn.Type().Out(0)), in.Len(), in.Len())
	}
	var ins [1]reflect.Value // Outside the loop to avoid one allocation.
	for i := 0; i < in.Len(); i++ {
		ins[0] = in.Index(i)
		out.Index(i).Set(fn.Call(ins[:])[0])
	}
	return out.Interface()
}

func chooseOrDropInPlace(slice, function interface{}, truth bool) {
	inp := reflect.ValueOf(slice)
	if inp.Kind() != reflect.Ptr {
		panic("choose/drop: not pointer to slice")
	}
	_, n := chooseOrDrop(inp.Elem().Interface(), function, true, truth)
	inp.Elem().SetLen(n)
}

func chooseOrDrop(slice, function interface{}, inPlace, truth bool) (interface{}, int) {
	// Special case for strings, very common.
	if strSlice, ok := slice.([]string); ok {
		if strFn, ok := function.(func(string) bool); ok {
			var r []string
			if inPlace {
				r = strSlice[:0]
			}
			for _, s := range strSlice {
				if strFn(s) == truth {
					r = append(r, s)
				}
			}
			return r, len(r)
		}
	}
	in := reflect.ValueOf(slice)
	if in.Kind() != reflect.Slice {
		panic("choose/drop: not slice")
	}
	fn := reflect.ValueOf(function)
	if fn.Kind() != reflect.Func {
		panic("choose/drop: not function")
	}
	if fn.Type().NumIn() != 1 || fn.Type().NumOut() != 1 || fn.Type().In(0) != in.Type().Elem() || fn.Type().Out(0).Kind() != reflect.Bool {
		panic("choose/drop: function must be of type func(" + in.Type().Elem().String() + ") bool")
	}
	var which []int
	var ins [1]reflect.Value // Outside the loop to avoid one allocation.
	for i := 0; i < in.Len(); i++ {
		ins[0] = in.Index(i)
		if fn.Call(ins[:])[0].Bool() == truth {
			which = append(which, i)
		}
	}
	out := in
	if !inPlace {
		out = reflect.MakeSlice(in.Type(), len(which), len(which))
	}
	for i := range which {
		out.Index(i).Set(in.Index(which[i]))
	}
	return out.Interface(), len(which)
}
