//go:build gui

package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/internal/auth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
)

// Run starts the Fyne GUI.
func Run() {
	a := app.NewWithID("com.cthrone.app")
	w := a.NewWindow("cthrone")
	w.Resize(fyne.NewSize(800, 600))

	// State
	portEntry := widget.NewEntry()
	portEntry.SetText("8080")

	hostEntry := widget.NewEntry()
	hostEntry.SetPlaceHolder("192.168.1.100 or leave empty for host mode")

	nameEntry := widget.NewEntry()
	nameEntry.SetPlaceHolder("Device name")

	tokenEntry := widget.NewPasswordEntry()
	tokenEntry.SetPlaceHolder("Shared secret (or leave empty to use file/env)")

	tlsCheck := widget.NewCheck("Enable TLS", nil)
	insecureCheck := widget.NewCheck("Insecure (skip cert verification)", nil)
	natsCheck := widget.NewCheck("Use NATS transport", nil)

	natsURLEntry := widget.NewEntry()
	natsURLEntry.SetText(natscore.DefaultBrokerURL)

	chatLog := widget.NewMultiLineEntry()
	chatLog.SetText("cthrone GUI\n")
	chatLog.Disable()

	msgEntry := widget.NewEntry()
	msgEntry.SetPlaceHolder("Type a message...")

	statusLabel := widget.NewLabel("Ready")

	// Discovery
	discoveryList := widget.NewList(
		func() int { return 0 },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {},
	)

	devices := []discovery.Beacon{}
	refreshDiscovery := func() {
		discoveryList.Length = func() int { return len(devices) }
		discoveryList.UpdateItem = func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < len(devices) {
				d := devices[id]
				obj.(*widget.Label).SetText(fmt.Sprintf("%s (%s:%d)", d.Name, d.Addr, d.Port))
			}
		}
		discoveryList.Refresh()
	}

	listener := discovery.NewListener()
	listener.OnFound(func(b discovery.Beacon) {
		devices = append(devices, b)
		refreshDiscovery()
		statusLabel.SetText(fmt.Sprintf("Discovered %s", b.Name))
	})
	listener.Start()

	scanBtn := widget.NewButton("Scan network", func() {
		statusLabel.SetText("Scanning...")
		// Discovery is passive via UDP beacon; also can trigger manual scan
		go func() {
			// Try to use scanner if available
			statusLabel.SetText("Listening for beacons on UDP 9999")
		}()
	})

	connectBtn := widget.NewButton("Connect", func() {
		if hostEntry.Text == "" {
			dialog.ShowError(fmt.Errorf("Enter host address"), w)
			return
		}
		statusLabel.SetText("Connecting...")
		go func() {
			p := 8080
			fmt.Sscanf(portEntry.Text, "%d", &p)
			appData := app.New(p)
			tlsConf := auth.TLS{
				Enabled:  tlsCheck.Checked,
				Insecure: insecureCheck.Checked,
			}
			var err error
			if natsCheck.Checked {
				args := []string{hostEntry.Text}
				natscore.Connect(args, appData, tokenEntry.Text, "", tlsConf)
			} else {
				err = websocketcore.Connect([]string{hostEntry.Text}, appData, tokenEntry.Text, "", tlsConf)
			}
			if err != nil {
				statusLabel.SetText(fmt.Sprintf("Error: %v", err))
				dialog.ShowError(err, w)
			} else {
				statusLabel.SetText("Connected")
			}
		}()
	})

	hostBtn := widget.NewButton("Host", func() {
		statusLabel.SetText("Starting host...")
		go func() {
			p := 8080
			fmt.Sscanf(portEntry.Text, "%d", &p)
			appData := app.New(p)
			appData.Name = nameEntry.Text
			tlsConf := auth.TLS{
				Enabled:  tlsCheck.Checked,
				Insecure: insecureCheck.Checked,
			}
			var err error
			if natsCheck.Checked {
				natscore.Host([]string{}, appData, natsURLEntry.Text, tlsConf)
			} else {
				err = websocketcore.Host(appData, tokenEntry.Text, "", tlsConf)
			}
			if err != nil {
				statusLabel.SetText(fmt.Sprintf("Error: %v", err))
				dialog.ShowError(err, w)
			} else {
				statusLabel.SetText("Hosting on port " + portEntry.Text)
			}
		}()
	})

	sendBtn := widget.NewButton("Send", func() {
		if msgEntry.Text == "" {
			return
		}
		chatLog.SetText(chatLog.Text + "\nYou: " + msgEntry.Text)
		msgEntry.SetText("")
	})

	// Layout
	settingsForm := widget.NewForm(
		&widget.FormItem{Text: "Port", Widget: portEntry},
		&widget.FormItem{Text: "Device Name", Widget: nameEntry},
		&widget.FormItem{Text: "Token/Secret", Widget: tokenEntry},
		&widget.FormItem{Text: "Host Address", Widget: hostEntry},
		&widget.FormItem{Text: "NATS URL", Widget: natsURLEntry},
	)

	tabs := container.NewAppTabs(
		container.NewTabItem("Chat", container.NewBorder(
			container.NewVBox(widget.NewLabel("Chat"), statusLabel),
			container.NewHBox(msgEntry, sendBtn),
			nil,
			nil,
			chatLog,
		)),
		container.NewTabItem("Devices", container.NewBorder(
			container.NewHBox(widget.NewLabel("Discovered Devices"), scanBtn),
			nil,
			nil,
			nil,
			discoveryList,
		)),
		container.NewTabItem("Host/Connect", container.NewVBox(
			widget.NewLabel("Actions"),
			container.NewHBox(hostBtn, connectBtn),
			widget.NewSeparator(),
			settingsForm,
			container.NewVBox(tlsCheck, insecureCheck, natsCheck),
		)),
	)

	w.SetContent(tabs)
	w.ShowAndRun()
}
