//go:build !gui

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

func init() {
	rootCmd.Run = func(cmd *cobra.Command, args []string) {
		if checkVersion {
			fmt.Println(Version)
			return
		}
	}
}
