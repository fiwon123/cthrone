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

var token string

var tokenFile string

var useTLS bool

var certFingerprint string

var insecure bool

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
	rootCmd.PersistentFlags().IntVarP(&port, "port", "p", 8080, "port to connect and host cthrone")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "the shared secret; it is visible in the process list, so prefer the environment or the file")
	rootCmd.PersistentFlags().BoolVar(&useTLS, "tls", false, "encrypt the transport: the host serves https, the client expects it")
	rootCmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "the file the shared secret is read from (default: the user configuration directory)")
	rootCmd.PersistentFlags().StringVar(&certFingerprint, "cert-fingerprint", "", "the fingerprint of the certificate to pin, with --tls")
	rootCmd.PersistentFlags().BoolVar(&insecure, "insecure", false, "accept any certificate, with --tls; the traffic is still encrypted, but the peer is not checked")
}
