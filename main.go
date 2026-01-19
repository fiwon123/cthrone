/*
Copyright © 2025 NAME HERE <EMAIL ADDRESS>
*/
package main

import (
	"os"
	"strings"

	"github.com/fiwon123/cthrone/cmd"
)

// Fixed termux android issue where first argument is the app filepath
func removeExtraPathArgument() {
	cleanArgs := []string{os.Args[0]}
	for _, a := range os.Args[1:] {
		if !strings.HasPrefix(a, "/") {
			cleanArgs = append(cleanArgs, a)
		}
	}
	os.Args = cleanArgs
}

func main() {
	removeExtraPathArgument()
	cmd.Execute()
}
