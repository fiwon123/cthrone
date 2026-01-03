package cmd

import (
	"fmt"
	"os"

	"github.com/fiwon123/cthrone/internal/core"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/spf13/cobra"
)

var connectIP string
var port int
var host bool

var checkVersion bool

// Version is popualated when building with Makefile
var Version = "vx.x.x"

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "cthrone",
	Short: "CThrone is a tool to use as an internal chat to send message from a device to another one.",
	Long:  `CThrone is a tool to manage how two or more internal devices communicate. You can configure protocol, encryption, output...`,

	Run: func(cmd *cobra.Command, args []string) {

		if checkVersion {
			fmt.Println(Version)
			return
		}

		app := app.New(port)

		if host {
			core.Host(app)
		} else if connectIP != "" {
			core.Connect(connectIP, app)
		} else {
			core.Scan()
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
	rootCmd.Flags().StringVarP(&connectIP, "connect", "c", "", "connect to a host cthrone ip")
	rootCmd.Flags().BoolVarP(&host, "host", "n", false, "host cthrone to receive connection")
	rootCmd.Flags().BoolVarP(&checkVersion, "version", "v", false, "check current version")
	rootCmd.Flags().IntVarP(&port, "port", "p", 8080, "port to connect and host ctrhone")
}
