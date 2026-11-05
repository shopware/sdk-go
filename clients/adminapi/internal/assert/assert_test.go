package assert

import (
	"testing"
	"unsafe"
)

func TestIsNil(t *testing.T) {
	var (
		nilPointer *int
		nilSlice   []int
		nilMap     map[string]int
		nilChan    chan int
		nilFunc    func()
		nilError   error
		nilUnsafe  unsafe.Pointer
	)
	one := 1

	tests := []struct {
		name string
		v    any
		want bool
	}{
		{"nil interface", nil, true},
		{"nil error", nilError, true},
		{"nil pointer", nilPointer, true},
		{"nil slice", nilSlice, true},
		{"nil map", nilMap, true},
		{"nil chan", nilChan, true},
		{"nil func", nilFunc, true},
		{"nil unsafe pointer", nilUnsafe, true},
		{"pointer", &one, false},
		{"empty slice", []int{}, false},
		{"empty map", map[string]int{}, false},
		{"zero int", 0, false},
		{"empty string", "", false},
		{"false", false, false},
		{"zero struct", struct{ A int }{}, false},
		{"zero array", [2]int{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNil(tt.v); got != tt.want {
				t.Errorf("isNil(%#v) = %v, want %v", tt.v, got, tt.want)
			}
		})
	}
}
