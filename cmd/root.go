package cmd

import (
	"fmt"
	"os"

	connectcmd "github.com/fiwon123/cthrone/cmd/connect"
	execcmd "github.com/fiwon123/cthrone/cmd/exec"
	hostcmd "github.com/fiwon123/cthrone/cmd/host"
	peerscmd "github.com/fiwon123/cthrone/cmd/peers"
	recvcmd "github.com/fiwon123/cthrone/cmd/recv"
	scancmd "github.com/fiwon123/cthrone/cmd/scan"
	uicmd "github.com/fiwon123/cthrone/cmd/ui"
	"github.com/spf13/cobra"
)

var port int

var token string

var tokenFile string

var useTLS bool

var noTLS bool

var certFingerprint string

var insecure bool

var checkVersion bool

// Version is popualated when building with Makefile
var Version = "vx.x.x"

// launchGUI opens the window. It is a variable so a test can prove the
// no-subcommand path reaches it: the failure this guards against is silent, so
// a test that only ran the command would see exit 0 and learn nothing.
var launchGUI = uicmd.Run

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

		// A command line with no subcommand opens the window. cthrone_gui is
		// this same binary built with -tags gui and -H=windowsgui, so a
		// double-click lands here with no console to explain itself: doing
		// nothing was indistinguishable from a broken application.
		launchGUI()

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
	rootCmd.AddCommand(peerscmd.Cmd)
	rootCmd.AddCommand(scancmd.Cmd)
	rootCmd.AddCommand(uicmd.Cmd)
	rootCmd.AddCommand(recvcmd.Cmd)
	rootCmd.AddCommand(execcmd.Cmd)

	rootCmd.Flags().BoolVarP(&checkVersion, "version", "v", false, "check current version")
	rootCmd.PersistentFlags().IntVarP(&port, "port", "p", 8080, "port to connect and host cthrone")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "the shared secret; it is visible in the process list, so prefer the environment or the file")
	rootCmd.PersistentFlags().BoolVar(&useTLS, "tls", false, "encrypt the transport (the default); kept so a command line that passes it still runs")
	rootCmd.PersistentFlags().BoolVar(&noTLS, "no-tls", false, "serve and dial plaintext ws:// instead of the default wss://; the payload stays sealed end to end")
	rootCmd.PersistentFlags().StringVar(&tokenFile, "token-file", "", "the file the shared secret is read from; no file is read unless it is named here")
	rootCmd.PersistentFlags().StringVar(&certFingerprint, "cert-fingerprint", "", "the fingerprint of the certificate to pin by hand; without one it is pinned on first contact")
	rootCmd.PersistentFlags().BoolVar(&insecure, "insecure", false, "accept any certificate; the traffic is still encrypted, but the peer's certificate is not checked")
}
