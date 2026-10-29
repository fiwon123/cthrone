/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"os"
	"strings"

	"github.com/fiwon123/cthrone/cmd"
)

// removeExtraPathArgument drops the stray path Termux puts in front of the
// command: there the first argument is the app's filepath, and cobra would
// read it as the command to run instead of the one after it.
//
// It considers that one argument only, and only when it is an absolute path.
// Filtering the whole command line would also swallow a flag value that is a
// path, which is how `--token-file /abs/path` and `--peers-file /abs/path`
// used to arrive with no value at all.
func removeExtraPathArgument() {
	if len(os.Args) < 2 || !strings.HasPrefix(os.Args[1], "/") {
		return
	}

	cleanArgs := make([]string, 0, len(os.Args)-1)
	cleanArgs = append(cleanArgs, os.Args[0])
	cleanArgs = append(cleanArgs, os.Args[2:]...)

	os.Args = cleanArgs
}

func main() {
	removeExtraPathArgument()
	cmd.Execute()
}
