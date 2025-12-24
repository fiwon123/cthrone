package cmd

import (
	"os"

	"github.com/fiwon123/cthrone/internal/core"
	"github.com/spf13/cobra"
)

var destIP string

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cthrone",
	Short: "CThrone is a tool to use as an internal chat to send message from a device to another one.",
	Long:  `CThrone is a tool to manage how two or more internal devices communicate. You can configure protocol, encryption, output...`,

	Run: func(cmd *cobra.Command, args []string) {
		core.Init(destIP)
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&destIP, "dest", "d", "", "Help message for toggle")
}
