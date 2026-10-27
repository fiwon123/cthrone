package main

import (
	"os"
	"reflect"
	"testing"
)

func TestRemoveExtraPathArgument(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	os.Args = []string{"cthrone", "/data/app/file", "host", "8080"}
	removeExtraPathArgument()
	want := []string{"cthrone", "host", "8080"}
	if !reflect.DeepEqual(os.Args, want) {
		t.Fatalf("got %v, want %v", os.Args, want)
	}
}

func TestRemoveExtraPathArgumentNoPaths(t *testing.T) {
	old := os.Args
	defer func() { os.Args = old }()

	os.Args = []string{"cthrone", "connect", "127.0.0.1:8080"}
	removeExtraPathArgument()
	want := []string{"cthrone", "connect", "127.0.0.1:8080"}
	if !reflect.DeepEqual(os.Args, want) {
		t.Fatalf("got %v, want %v", os.Args, want)
	}
}
