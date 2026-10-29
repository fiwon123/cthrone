package main

import (
	"os"
	"reflect"
	"testing"
)

// The workaround is for one argument in one position: the app filepath Termux
// puts in front of the command. Everything after it is the user's command line
// and has to reach cobra untouched — a flag value that is an absolute path was
// being dropped with it, so `--peers-file /abs/path` and `--token-file
// /abs/path` arrived with no value and the command refused to run.
func TestRemoveExtraPathArgument(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want []string
	}{
		{
			name: "the termux prefix is removed",
			args: []string{"cthrone", "/data/data/com.termux/files/usr/bin/cthrone", "host", "8080"},
			want: []string{"cthrone", "host", "8080"},
		},
		{
			name: "a value given as its own token survives",
			args: []string{"cthrone", "peers", "--peers-file", "/tmp/pins.json"},
			want: []string{"cthrone", "peers", "--peers-file", "/tmp/pins.json"},
		},
		{
			name: "a value attached with = survives",
			args: []string{"cthrone", "peers", "--peers-file=/tmp/pins.json"},
			want: []string{"cthrone", "peers", "--peers-file=/tmp/pins.json"},
		},
		{
			name: "the other path flag survives too",
			args: []string{"cthrone", "host", "--token-file", "/etc/cthrone/token"},
			want: []string{"cthrone", "host", "--token-file", "/etc/cthrone/token"},
		},
		{
			name: "nothing that looks like a path",
			args: []string{"cthrone", "connect", "127.0.0.1:8080"},
			want: []string{"cthrone", "connect", "127.0.0.1:8080"},
		},
		{
			name: "the prefix and nothing else",
			args: []string{"cthrone", "/data/app/file"},
			want: []string{"cthrone"},
		},
		{
			name: "no arguments at all",
			args: []string{"cthrone"},
			want: []string{"cthrone"},
		},
		{
			name: "the prefix in front of a command that takes a path",
			args: []string{"cthrone", "/data/app/file", "peers", "--peers-file", "/tmp/pins.json"},
			want: []string{"cthrone", "peers", "--peers-file", "/tmp/pins.json"},
		},
		{
			name: "a path in a command's own arguments is not a prefix",
			args: []string{"cthrone", "exec", "laptop", "--", "cat", "/etc/hosts"},
			want: []string{"cthrone", "exec", "laptop", "--", "cat", "/etc/hosts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original := os.Args
			defer func() { os.Args = original }()

			os.Args = append([]string(nil), tt.args...)
			removeExtraPathArgument()

			if !reflect.DeepEqual(os.Args, tt.want) {
				t.Fatalf("removeExtraPathArgument(%v) = %v, want %v", tt.args, os.Args, tt.want)
			}
		})
	}
}
