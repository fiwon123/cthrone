package cmd

import (
	"fmt"
	"os"

	connectcmd "github.com/fiwon123/cthrone/cmd/connect"
	hostcmd "github.com/fiwon123/cthrone/cmd/host"
	scancmd "github.com/fiwon123/cthrone/cmd/scan"
	"github.com/spf13/cobra"
)

var port int

var checkVersion bool

// Version is popualated when building with Makefile
var Version = "vx.x.x"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cthrone",
	Short: "CThrone is a tool to use as an internal chat to send message from a device to another one.",
	Long:  `CThrone is a tool to manage how two or more internal devices communicate.`,

	Run: func(cmd *cobra.Command, args []string) {

		if checkVersion {
			fmt.Println(Version)
			return
		}

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
	rootCmd.AddCommand(connectcmd.Cmd)
	rootCmd.AddCommand(hostcmd.Cmd)
	rootCmd.AddCommand(scancmd.Cmd)

	rootCmd.Flags().BoolVarP(&checkVersion, "version", "v", false, "check current version")
	rootCmd.PersistentFlags().IntVarP(&port, "port", "p", 8080, "port to connect and host ctrhone")
}
