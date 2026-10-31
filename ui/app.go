//go:build gui

// Package ui is the Fyne GUI.
//
// It is built with -tags gui: without that tag ui.Run is the stub in
// app_nogui.go and the CLI does not need the GUI toolkit. The desktop app and
// the Android/iOS app share this file; the only differences are the entry
// point (cmd/ui for the desktop, mobile/main.go for the APK) and the layout
// adjustments made on a narrow screen (see layoutFor in layout.go).
package ui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/internal/auth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	appdata "github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// copyHintDuration is how long the transcript's "Copied" confirmation stays on
// screen. A tap that shows nothing reads as a tap that did nothing.
const copyHintDuration = 2 * time.Second

// liveServer is the part of a websocket host the GUI needs. Keeping it an
// interface means the UI does not import the handler package.
type liveServer interface {
	Addr() string
	Stop() error
}

// gui holds the widgets and the live state of one window.
type gui struct {
	app    fyne.App
	win    fyne.Window
	mobile bool

	// lay is how the rows are arranged for this screen. It is decided once,
	// while the window is built: on a phone the canvas has no size yet, so
	// layoutFor treats it as narrow, and the rows stay stacked in both
	// orientations instead of reflowing under the user's finger.
	lay screenLayout

	// setup
	portEntry        *widget.Entry
	bindEntry        *widget.Entry
	nameEntry        *widget.Entry
	hostEntry        *widget.Entry
	tokenEntry       *widget.Entry
	fingerprintEntry *widget.Entry
	subjectEntry     *widget.Entry
	natsURLEntry     *widget.Entry
	tlsCheck         *widgets.CheckRow
	insecureCheck    *widgets.CheckRow
	natsCheck        *widgets.CheckRow

	// setup layout: the form is rebuilt whenever the mode changes, because a
	// Fyne Form always renders a row for every item it holds.
	moreOptions bool
	moreBtn     *widget.Button
	formHolder  *fyne.Container
	transport   *widget.Select
	hint        *widget.Label

	// thisDevice is the read-only line that shows a hosting device its own
	// address. It is hidden until this device hosts: the Devices list never
	// lists this device, so this is where its address lives.
	thisDevice *widget.Label

	// credential is the read-only line that names the credential in force and
	// where it came from — this device's key, or a shared secret from the
	// field, the environment or the token file. A token file a user forgot
	// about otherwise switches the device into the secret model in silence,
	// and a key-paired peer is then refused with no explanation on screen.
	credential *widget.Label

	// forgetSecret deletes the token file that is in charge. It is shown only
	// while that file is what decides the credential, because deleting it in
	// any other case would change nothing the user could see.
	forgetSecret *widget.Button

	// about is the read-only tab: which build this is, where it comes from,
	// and — on a press, never on a start — whether there is a newer release.
	// aboutCredential repeats the Setup tab's line, because "which model am I
	// in?" belongs on the screen a bug report is written from.
	aboutCredential *widget.Label
	aboutNote       *widget.Label
	updateBtn       *widget.Button

	// chat
	tabs      *container.AppTabs
	status    *widget.Label
	chatList  *widget.List
	entry     *widget.Entry
	sendBtn   *widget.Button
	stopBtn   *widget.Button
	attachBtn *widget.Button
	copyHint  *widget.Label
	messages  []chatMessage

	// transfers
	//
	// outbox is the queue the picker feeds and one worker drains, so two files
	// picked in a row are two transfers in order rather than two interleaved
	// streams. cancels is how Stop reaches a transfer that is still running.
	outbox    chan outgoing
	cancels   map[string]context.CancelFunc
	incoming  map[string]*incomingTransfer
	assembler *wire.Assembler

	// devices
	beacons    []device
	beaconList *widget.List
	scanBtn    *widget.Button
	listener   *discovery.Listener

	mu     sync.Mutex
	sess   session.Session
	server liveServer
}

// Run starts the Fyne GUI.
//
// A window is started by a click, so there is no console to print to and no
// exit code anybody reads: a failure that is only returned is a failure nobody
// sees. Anything that stops the GUI therefore goes to logFailure, which writes
// it beside the device's key and prints it for a binary started from a
// terminal, instead of vanishing into an empty recover().
func Run() {
	logEvent("gui start")
	if err := run(); err != nil {
		logFailure(err)
		notifyFailure(err)
	}
}

// run builds the window and hands the event loop to the toolkit. It returns the
// failure that stopped it rather than swallowing it.
//
// A panic is a failure too: the toolkit and the platform underneath it are not
// this program's code, and a nil window from a driver that could not start is
// exactly the case that used to exit 0 in silence.
func run() (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the GUI stopped: %v\n%s", r, debug.Stack())
		}
	}()

	a := fyneapp.NewWithID("com.cthrone.app")
	if a == nil {
		return errors.New("the GUI toolkit did not create an application")
	}

	if ic := appIcon(); ic != nil {
		a.SetIcon(ic)
	}

	// The device is asked *after* the app exists: fyne.CurrentDevice goes
	// through the current app, so before this point there is nothing to ask and
	// the call panics — which is why the answer used to be "not mobile" on
	// every device, including the phone.
	mobile := currentDeviceIsMobile()

	w := a.NewWindow("cthrone")
	if w == nil {
		return errors.New("the GUI toolkit did not create a window")
	}

	if ic := appIcon(); ic != nil {
		w.SetIcon(ic)
	}

	g := &gui{app: a, win: w, mobile: mobile}

	// A window that cannot be built has nothing to show, and a blank window is
	// a worse answer than a report: the panic propagates to the recover above.
	g.build()

	go g.pruneLoop()

	if mobile {
		// Discovery is skipped on a phone: the beacon and the scan are for
		// finding another machine on a network, and the app that is already
		// open is the one the user picked up.
	} else {
		// Discovery and the resize are not worth the window: a failure in
		// either is recorded and the chat still opens.
		if err := guard("discovery", g.startDiscovery); err != nil {
			logFailure(err)
		}

		if err := guard("resize", func() { w.Resize(fyne.NewSize(900, 640)) }); err != nil {
			logFailure(err)
		}
	}

	w.ShowAndRun()

	return nil
}

// currentDeviceIsMobile reports whether this is a touch device.
//
// "Not mobile" is the answer when the toolkit cannot say: a desktop layout on a
// phone is a bad layout, while a phone layout on a desktop is a window with
// stacked rows, and only one of those is unusable.
func currentDeviceIsMobile() (mobile bool) {
	defer func() {
		if r := recover(); r != nil {
			mobile = false
		}
	}()

	if dev := fyne.CurrentDevice(); dev != nil {
		return dev.IsMobile()
	}

	return false
}

// build creates every widget and lays the window out.
func (g *gui) build() {
	g.lay = layoutFor(g.mobile, g.canvasWidth())

	// --- chat ---------------------------------------------------------
	// The desktop window already says "cthrone" in its title bar, so the header
	// carries only what a title bar cannot: the name on a phone, which has no
	// title bar to show it in, the copy confirmation, and the copy-all action.
	g.copyHint = selectableLabel("")

	copyLabel := "Copy all"
	if g.lay.Stack {
		// The icon is enough where width is scarce.
		copyLabel = ""
	}

	copyBtn := widget.NewButtonWithIcon(copyLabel, theme.ContentCopyIcon(), g.copyAll)

	// The Files action lives beside the transcript rather than in Setup: it is
	// about what happens to a received file, which is a chat concern, and Setup
	// is the connection form.
	filesLabel := "Files"
	if g.lay.Stack {
		filesLabel = ""
	}

	filesBtn := widget.NewButtonWithIcon(filesLabel, theme.FolderIcon(), g.showFiles)

	header := container.NewHBox()
	if g.mobile {
		header.Add(widget.NewLabelWithStyle("cthrone", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	}

	header.Add(layout.NewSpacer())
	header.Add(g.copyHint)
	header.Add(filesBtn)
	header.Add(copyBtn)

	g.status = selectableLabel("Ready — open Setup to connect or host.")
	g.status.TextStyle = fyne.TextStyle{Bold: true}

	// One row per message, not one label holding the whole log: appending then
	// costs a row instead of a re-wrap of every line, and a row is a tap target
	// a finger can hit.
	//
	// A row is a container rather than a label because a file transfer needs
	// more than a sentence: a picture draws itself under the caption, and a
	// received file offers the Save action the terminal has no room for. Both
	// parts stay hidden on a text row, so a message costs the same two hidden
	// objects it did not have before and nothing else.
	g.chatList = widget.NewList(
		func() int { return len(g.messages) },
		func() fyne.CanvasObject {
			row := selectableLabel("")

			pic := canvas.NewImageFromResource(nil)
			pic.FillMode = canvas.ImageFillContain
			pic.Hide()

			save := widget.NewButtonWithIcon("Save as…", theme.DocumentSaveIcon(), nil)
			save.Hide()

			return container.NewVBox(row, pic, save)
		},
		g.updateChatRow,
	)
	g.chatList.OnSelected = g.selectChatRow

	g.entry = widget.NewEntry()
	g.entry.SetPlaceHolder("Type a message…")
	g.entry.OnSubmitted = func(text string) { g.send(text) }

	g.sendBtn = widget.NewButtonWithIcon("Send", theme.MailSendIcon(), func() { g.send(g.entry.Text) })
	g.sendBtn.Disable()

	g.stopBtn = widget.NewButtonWithIcon("Stop", theme.MediaStopIcon(), g.stop)
	g.stopBtn.Disable()

	attachLabel := "Attach"
	if g.lay.Stack {
		// The paperclip says it on a phone, where the row has no width to spare.
		attachLabel = ""
	}

	g.attachBtn = widget.NewButtonWithIcon(attachLabel, theme.MailAttachmentIcon(), g.pickFile)
	g.attachBtn.Disable()

	// The queue starts with the window and outlives any one session: a file
	// picked while the connection is being replaced still has a worker to hand
	// it to, and that worker reports the failure if there is nothing to send to.
	g.outbox = make(chan outgoing, 32)
	go g.pumpOutbox()

	var input fyne.CanvasObject
	if g.lay.Stack {
		// Touch: a full-width field, and the two actions below it, stacked
		// one per line when even two do not fit.
		input = container.NewVBox(g.entry, container.NewGridWithColumns(g.lay.ActionColumns, g.stopBtn, g.attachBtn, g.sendBtn))
	} else {
		input = container.NewBorder(nil, nil, nil, container.NewHBox(g.stopBtn, g.attachBtn, g.sendBtn), g.entry)
	}

	chat := container.NewBorder(
		container.NewVBox(container.NewPadded(header), container.NewPadded(g.status), widget.NewSeparator()),
		container.NewPadded(input),
		nil, nil,
		g.chatList,
	)

	// --- setup --------------------------------------------------------
	g.portEntry = widget.NewEntry()
	g.portEntry.SetText(strconv.Itoa(defaultPort))

	g.bindEntry = widget.NewEntry()
	g.bindEntry.SetPlaceHolder("every interface")

	g.nameEntry = widget.NewEntry()
	if identity, err := discovery.LoadIdentity(); err == nil && identity.Name != "" {
		g.nameEntry.SetText(identity.Name)
	} else {
		g.nameEntry.SetPlaceHolder("device name")
	}

	g.hostEntry = widget.NewEntry()
	g.hostEntry.SetPlaceHolder("192.168.1.100")

	g.tokenEntry = widget.NewPasswordEntry()
	g.tokenEntry.SetPlaceHolder("empty: the token file, else key pairing")
	g.tokenEntry.OnChanged = func(string) { g.refreshCredential() }

	g.fingerprintEntry = widget.NewEntry()
	g.fingerprintEntry.SetPlaceHolder("certificate fingerprint (optional: pinned on first contact)")

	g.subjectEntry = widget.NewEntry()
	g.subjectEntry.SetText(natscore.DefaultSubject)

	g.natsURLEntry = widget.NewEntry()
	g.natsURLEntry.SetText(natscore.DefaultBrokerURL)

	g.tlsCheck = widgets.NewCheckRow("Encrypt with TLS", func(bool) { g.rebuildSetupForm() })
	// Encrypted is the default on the CLI, so the app starts the same way: a
	// phone that has to find a switch before it is protected is not protected.
	g.tlsCheck.SetChecked(true)
	g.insecureCheck = widgets.NewCheckRow("Skip the certificate check (insecure)", nil)
	g.natsCheck = widgets.NewCheckRow("Use NATS instead of websocket", nil)

	g.moreBtn = widget.NewButton("More options", func() {
		g.moreOptions = !g.moreOptions
		g.rebuildSetupForm()
	})

	g.transport = widget.NewSelect([]string{"WebSocket (default)", "NATS"}, func(s string) {
		g.natsCheck.SetChecked(s == "NATS")
		g.rebuildSetupForm()
	})
	g.transport.SetSelected("WebSocket (default)")

	// The form is rebuilt into this stack: a Fyne form has no way to drop a
	// single row, so the visible subset becomes a fresh form instead.
	g.formHolder = container.NewStack()

	actions := container.NewGridWithColumns(2,
		widget.NewButtonWithIcon("Host", theme.MediaPlayIcon(), g.host),
		widget.NewButtonWithIcon("Connect", theme.LoginIcon(), g.connect),
	)

	g.hint = selectableLabel("")

	g.thisDevice = selectableLabel("")
	g.thisDevice.Hide()

	g.credential = selectableLabel("")

	g.forgetSecret = widget.NewButtonWithIcon("Stop using the shared secret", theme.ContentClearIcon(), g.forgetSecretFile)
	g.forgetSecret.Hide()

	setup := container.NewVScroll(container.NewPadded(container.NewVBox(
		widgets.SelectableHeading("Quick Setup"),
		g.tlsCheck,
		g.insecureCheck,
		g.formHolder,
		g.credential,
		g.forgetSecret,
		widget.NewSeparator(),
		actions,
		g.thisDevice,
		g.hint,
	)))

	g.rebuildSetupForm()

	// --- devices ------------------------------------------------------
	g.scanBtn = widget.NewButtonWithIcon("Scan network", theme.ViewRefreshIcon(), g.scan)

	// A beacon row stays a plain label: the list gives it one fixed line, and a
	// wrapping selectable label would clip there. The address is copyable from
	// the "Selected" status line and the dial field, which is where a user who
	// wants it is already looking.
	g.beaconList = widget.NewList(
		func() int { return len(g.beacons) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			if id < 0 || id >= len(g.beacons) {
				return
			}

			beacon := g.beacons[id]
			if lbl, ok := obj.(*widget.Label); ok {
				lbl.SetText(fmt.Sprintf("%s — %s:%d", beacon.Name, beacon.Addr, beacon.Port))
			}
		},
	)
	g.beaconList.OnSelected = g.selectBeacon

	// The beacon hint wraps on a phone, where the button and the sentence do
	// not share a line without the sentence running off the screen.
	var beaconHeader fyne.CanvasObject
	beaconHint := selectableLabel("UDP beacons on port 9999")
	if g.lay.Stack {
		beaconHeader = container.NewVBox(g.scanBtn, beaconHint)
	} else {
		beaconHeader = container.NewHBox(g.scanBtn, beaconHint)
	}

	devices := container.NewBorder(
		container.NewVBox(
			container.NewPadded(beaconHeader),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		g.beaconList,
	)

	// On a narrow screen an icon costs the width its label needs, so the tabs
	// keep the names and drop the icons; the label alone is still the whole
	// target.
	tab := func(label string, icon fyne.Resource, content fyne.CanvasObject) *container.TabItem {
		if g.lay.Stack {
			return container.NewTabItem(label, content)
		}

		return container.NewTabItemWithIcon(label, icon, content)
	}

	g.tabs = container.NewAppTabs(
		tab("Chat", theme.MailComposeIcon(), chat),
		tab("Setup", theme.SettingsIcon(), setup),
		tab("Devices", theme.ComputerIcon(), devices),
		tab("About", theme.InfoIcon(), g.buildAbout()),
	)

	g.win.SetContent(g.tabs)
	g.registerTabShortcuts()
}

// registerTabShortcuts makes the tab bar reachable without the pointer.
//
// Plain Tab is deliberately left alone: inside a form it moves focus between
// widgets, and the Setup tab is a form. Ctrl+Tab is the page-switch gesture
// browsers and editors already teach, and Ctrl+←/→ is the arrow-key spelling of
// the same move. None of the four is taken by Fyne: the driver only routes
// Tab+Shift to focus traversal.
func (g *gui) registerTabShortcuts() {
	canvas := g.win.Canvas()
	if canvas == nil {
		return
	}

	canvas.AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyTab, Modifier: fyne.KeyModifierControl},
		func(fyne.Shortcut) { g.cycleTab(1) },
	)
	canvas.AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyTab, Modifier: fyne.KeyModifierControl | fyne.KeyModifierShift},
		func(fyne.Shortcut) { g.cycleTab(-1) },
	)
	canvas.AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyRight, Modifier: fyne.KeyModifierControl},
		func(fyne.Shortcut) { g.cycleTab(1) },
	)
	canvas.AddShortcut(
		&desktop.CustomShortcut{KeyName: fyne.KeyLeft, Modifier: fyne.KeyModifierControl},
		func(fyne.Shortcut) { g.cycleTab(-1) },
	)
}

// cycleTab moves delta tabs along the ring, wrapping at both ends.
func (g *gui) cycleTab(delta int) {
	if g.tabs == nil {
		return
	}

	g.tabs.SelectIndex(nextTabIndex(g.tabs.SelectedIndex(), len(g.tabs.Items), delta))
}

// rebuildSetupForm lays out the rows the current configuration uses.
//
// The rows are rebuilt from the visible subset rather than hidden one by one:
// the entries themselves live on g, so the text already typed into them
// survives, and a row that is not in the subset leaves nothing behind.
func (g *gui) rebuildSetupForm() {
	// The transport's OnSelected runs while build() is still wiring the tab.
	if g.formHolder == nil {
		return
	}

	useNATS := g.natsCheck.Checked()
	view := setupVisibility(useNATS, g.tlsCheck.Checked(), g.moreOptions)

	items := []*widget.FormItem{
		widgets.FormRow("Device name", g.nameEntry),
		widgets.FormRow(labelConnectTo, g.hostEntry),
		widgets.FormRow("Port", g.portEntry),
		widgets.FormRow("Transport", g.transport),
	}
	if view.Options {
		items = append(items, widgets.FormRow("Options", g.moreBtn))
	}
	if view.SharedSecret {
		items = append(items, widgets.FormRow("Shared secret", g.tokenEntry))
	}
	if view.Fingerprint {
		items = append(items, widgets.FormRow("Fingerprint", g.fingerprintEntry))
	}
	if view.NATSBroker {
		items = append(items, widgets.FormRow("NATS URL", g.natsURLEntry))
	}
	if view.NATSSubject {
		items = append(items, widgets.FormRow("NATS subject", g.subjectEntry))
	}
	if view.BindAddress {
		items = append(items, widgets.FormRow("Bind address", g.bindEntry))
	}

	// A check row hides its caption with the box, so the whole row goes.
	if view.Insecure {
		g.insecureCheck.Show()
	} else {
		g.insecureCheck.Hide()
	}

	if g.moreOptions {
		g.moreBtn.SetText("Fewer options")
	} else {
		g.moreBtn.SetText("More options")
	}

	g.hint.SetText(setupHint(useNATS))

	g.formHolder.Objects = []fyne.CanvasObject{widget.NewForm(items...)}
	g.formHolder.Refresh()

	g.refreshCredential()
}

// --- actions ----------------------------------------------------------

// connect joins a host (websocket) or a broker (NATS).
func (g *gui) connect() {
	host := strings.TrimSpace(g.hostEntry.Text)
	useNATS := g.natsCheck.Checked()

	if !useNATS && host == "" {
		g.showError(errors.New("enter the host address to connect to"))

		return
	}

	data := g.data()
	tlsConf := g.tls()
	opts := g.pairingOptions()
	broker := g.natsURLEntry.Text
	subject := g.subject()

	g.setStatus("Connecting…")
	g.tabs.SelectIndex(0)

	go func() {
		var (
			sess session.Session
			err  error
		)

		if useNATS {
			sess, err = natscore.OpenSession([]string{subject}, data, opts, broker, tlsConf)
		} else {
			sess, err = websocketcore.Dial([]string{host}, data, opts, tlsConf)
		}

		if err != nil {
			g.setStatus("Connection failed")
			g.showError(err)

			return
		}

		if useNATS {
			g.setStatus("Connected through NATS on " + broker)
		} else {
			g.setStatus("Connected to " + host + " — chatting")
		}

		g.noteFirstContact(sess)
		g.attach(sess)
	}()
}

// noteFirstContact shows the code a joining device is compared by while the
// host decides.
//
// Only the side that dials waits on a person: the host is the side that is
// asked, so it has a dialog and needs no waiting line. A bus has no code and no
// single person to ask, so nothing is shown there.
func (g *gui) noteFirstContact(sess session.Session) {
	sealed, ok := sess.(*e2e.Conn)
	if !ok {
		return
	}

	sealed.OnFirstContact(func(c e2e.FirstContact) { g.setStatus(waitingText(c)) })
}

// host starts a host (websocket) or joins the broker as a peer (NATS).
func (g *gui) host() {
	data := g.data()
	tlsConf := g.tls()
	opts := g.pairingOptions()
	broker := g.natsURLEntry.Text
	subject := g.subject()
	useNATS := g.natsCheck.Checked()
	typedToken := strings.TrimSpace(g.tokenEntry.Text) != ""

	// This device's own address, as the other device's scan will find it. The
	// default route picks the network the device is actually on, so a docker
	// bridge or a VPN adapter is not advertised as a way in. Both are read
	// before the goroutine so no interface is touched off the UI thread, and
	// they are only meaningful for the websocket host: the NATS path has a
	// broker, not an address on this network.
	route, _ := defaultRouteIP()
	addrs := discovery.AdvertisedAddrs(localAddrs(), route)
	own := thisDeviceText(addrs, data.Port)
	hosting := "Hosting — waiting for connection"
	if addresses := deviceAddresses(addrs, data.Port); addresses != "" {
		hosting = "Hosting on " + addresses + " — waiting for connection"
	}

	g.setStatus("Starting…")

	go func() {
		if useNATS {
			sess, err := natscore.OpenSession([]string{subject}, data, opts, broker, tlsConf)
			if err != nil {
				g.setStatus("Host failed")
				g.showError(err)

				return
			}

			g.setStatus("Hosting through NATS on " + broker)
			g.attach(sess)
			g.tabs.SelectIndex(0)

			return
		}

		handle, err := websocketcore.OpenServer(data, opts, tlsConf, func(sess session.Session) {
			g.setStatus("A device joined — chatting")
			g.attach(sess)
		}, g.confirmFirstContact)
		if err != nil {
			g.setStatus("Host failed")
			g.showError(err)

			return
		}

		g.mu.Lock()
		g.server = handle
		g.mu.Unlock()

		fyne.Do(g.updateButtons)
		g.showThisDevice(own)
		g.setStatus(hosting)
		g.tabs.SelectIndex(0)
		g.offerPairing(handle.Token, handle.DeviceFingerprint, handle.Fingerprint, own, !typedToken)
	}()
}

// showThisDevice puts this device's own address on screen while it hosts. The
// Devices list never lists this device, so this line is where a user reads the
// address the other device connects to.
func (g *gui) showThisDevice(text string) {
	if g.thisDevice == nil || text == "" {
		return
	}

	fyne.Do(func() {
		g.thisDevice.SetText(text)
		g.thisDevice.Show()
	})
}

// hideThisDevice takes the address line away when this device stops hosting: it
// is a fact about the running host, not a permanent setting.
func (g *gui) hideThisDevice() {
	if g.thisDevice == nil {
		return
	}

	g.thisDevice.SetText("")
	g.thisDevice.Hide()
}

// offerPairing shows the values the other device needs. The terminal host
// prints them; a GUI has no terminal, and on Android there is no file to open,
// so this dialog is the only way to pair a phone that hosts.
//
// There are two models and the host uses exactly one: with a shared secret the
// other device types it; without a secret nothing is copied at all, and the
// two devices compare the fingerprint each shows on first contact. The
// transport is encrypted either way, and the client pins the host's
// certificate on first contact unless one is typed into the fingerprint field.
//
// own is this device's own address line (thisDeviceText), which is the address
// the other device's scan finds: the Devices list never lists this device, so
// the dialog is where a hosting user reads it.
func (g *gui) offerPairing(token, deviceFingerprint, certificateFingerprint, own string, showToken bool) {
	var message strings.Builder

	if showToken && token != "" {
		fmt.Fprintf(&message, "Shared secret:\n%s\n\n", token)
	}

	if token == "" && deviceFingerprint != "" {
		fmt.Fprintf(&message, "This device's fingerprint:\n%s\n\n", deviceFingerprint)
		message.WriteString("Nothing to copy: on the other device, connect to this one and compare the fingerprint it shows. It is remembered, and a different key under this name is refused.\n\n")
	}

	if certificateFingerprint != "" {
		fmt.Fprintf(&message, "Certificate fingerprint:\n%s\n\n", certificateFingerprint)
		message.WriteString("The other device pins this certificate on first contact: compare the two values there if you want to be sure.\n\n")
	}

	if own != "" {
		fmt.Fprintf(&message, "%s\n\n", own)

		if token != "" {
			message.WriteString("That is the address the other device connects to; enter the shared secret there.\n\n")
		} else {
			message.WriteString("That is the address the other device connects to; compare the fingerprint both screens show.\n\n")
		}
	}

	if message.Len() == 0 {
		return
	}

	if token != "" {
		message.WriteString("Enter the shared secret on the other device.\n")
	}

	text := message.String()

	fyne.Do(func() {
		if showToken && token != "" {
			g.tokenEntry.SetText(token)
			g.app.Clipboard().SetContent(token)
		}

		g.showCopyable("Pairing", text)
	})
}

// stop closes the live session and stops the host, if any.
//
// A transfer in flight is cancelled first: it is the one action whose value is
// a half-written file, and leaving it running would leave one behind.
func (g *gui) stop() {
	g.cancelTransfers()

	g.mu.Lock()
	sess, server := g.sess, g.server
	g.sess, g.server = nil, nil
	g.mu.Unlock()

	g.finishReceive()

	g.updateButtons()
	g.hideThisDevice()
	g.setStatus("Disconnected")

	go func() {
		if sess != nil {
			sess.Close("closed from the app")
		}

		if server != nil {
			_ = server.Stop()
		}
	}()
}

// send publishes the typed text to the live session.
func (g *gui) send(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	g.mu.Lock()
	sess := g.sess
	g.mu.Unlock()

	if sess == nil {
		g.setStatus("Nothing to send to — connect, or wait for a device to join")

		return
	}

	// Text goes through the envelope so the one string the receiver would
	// misread — a message beginning with the magic — is refused here, where the
	// user can be told, instead of arriving as a damaged frame. For every other
	// message Encode hands the bytes back unchanged, so a peer that predates the
	// envelope still reads it.
	payload, err := wire.Encode(wire.Header{Kind: wire.KindText}, []byte(text))
	if err != nil {
		g.setStatus("Not sent: " + err.Error())

		return
	}

	g.entry.SetText("")
	g.addMessage(chatMessage{text: text})

	go func() {
		if err := sess.Send(payload); err != nil {
			g.setStatus("Send failed: " + err.Error())
		}
	}()
}

// scan walks the local subnet for a chat host on the configured port.
//
// The devices already listed are re-checked too: a host that was stopped
// since it was heard fails the check and leaves before its TTL would have
// expired it, so pressing the button never keeps a dead address around.
func (g *gui) scan() {
	data := g.data()

	g.scanBtn.Disable()
	g.setStatus(fmt.Sprintf("Scanning port %d…", data.Port))

	// The list belongs to the UI goroutine, so the check works on a copy.
	checked := append([]device(nil), g.beacons...)

	go func() {
		answered := checkDevices(checked, dialDevice, scanDialTimeout)
		found := websocketcore.Scan(data)
		now := time.Now()

		fyne.Do(func() {
			g.scanBtn.Enable()

			g.beacons = withoutDead(g.beacons, checked, answered)
			g.beacons = addScanned(g.beacons, found, data.Port, now)
			g.beaconList.Refresh()

			if len(found) == 0 {
				g.setStatus("No host answered on this network")

				return
			}

			g.setStatus(fmt.Sprintf("Found %d host(s) — tap one to connect", len(found)))
		})
	}()
}

// selectBeacon fills the host address from a discovered device.
func (g *gui) selectBeacon(id widget.ListItemID) {
	if id < 0 || id >= len(g.beacons) {
		return
	}

	beacon := g.beacons[id]

	g.hostEntry.SetText(beacon.Addr)
	if beacon.Port > 0 {
		g.portEntry.SetText(strconv.Itoa(beacon.Port))
	}

	g.setStatus("Selected " + beacon.Name + " (" + beacon.Addr + ")")
	g.tabs.SelectIndex(1)
	g.beaconList.UnselectAll()
}

// --- session plumbing -------------------------------------------------

// attach makes sess the live session and renders what it carries.
func (g *gui) attach(sess session.Session) {
	// A session owns its assembler: a half-received file belongs to the
	// connection that was carrying it, and the next connection starts clean.
	g.prepareReceive()

	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	fyne.Do(g.updateButtons)

	// A sealed session reports a peer the moment its handshake authenticates,
	// and a bus refusal that dropped one peer without ending the room. Both
	// belong on the status line: a silent drop looks like a peer that never
	// sent anything, and "connected" is not the same as "encrypted".
	if sealed, ok := sess.(*e2e.Conn); ok {
		sealed.OnPeer(func(p e2e.Peer) {
			if p.First && p.Fingerprint != "" {
				// First contact is the one moment the fingerprint can be
				// compared, so it goes on screen and in the transcript; a
				// later contact only says who it is. Both values are shown,
				// because the other screen has them under the opposite labels.
				g.setStatus("Paired with " + p.Name())
				g.addMessage(chatMessage{
					from: "system",
					text: "paired with " + p.Name() + " — first contact; its fingerprint is " + p.Fingerprint +
						", this device's is " + sealed.Fingerprint() + ". Compare both with the other screen.",
				})

				return
			}

			g.setStatus("Encrypted with " + p.Name())
			g.addMessage(chatMessage{from: "system", text: "encrypted with " + p.Name()})
		})
		sealed.OnError(func(err error) {
			g.setStatus("Refused a peer: " + err.Error())
			g.addMessage(chatMessage{from: "system", text: "refused a peer: " + err.Error()})
		})
	}

	sess.OnMessage(func(m session.Message) {
		from := m.From
		if from == "" {
			// An unnamed peer is still a peer: without this it would render as
			// "You", which is the one sender it is not.
			from = "peer"
		}

		// A payload is a chat message or a piece of a file: the envelope says
		// which, and text that predates it still arrives as text.
		g.receiveFrame(from, m.Payload)
	})

	sess.OnClose(func(reason string) {
		g.mu.Lock()
		closed := g.sess == sess
		if closed {
			g.sess = nil
		}
		g.mu.Unlock()

		if closed {
			g.finishReceive()
		}

		fyne.Do(g.updateButtons)
		g.setStatus("Disconnected: " + reason)
	})
}

// updateButtons enables what the current state can actually do. Send needs a
// session; Stop also makes sense for a host with nobody connected yet.
func (g *gui) updateButtons() {
	g.mu.Lock()
	hasSession := g.sess != nil
	hasServer := g.server != nil
	g.mu.Unlock()

	if hasSession {
		g.sendBtn.Enable()
		g.attachBtn.Enable()
	} else {
		g.sendBtn.Disable()
		g.attachBtn.Disable()
	}

	if hasSession || hasServer {
		g.stopBtn.Enable()
	} else {
		g.stopBtn.Disable()
	}
}

// canvasWidth reports the width of the window, or 0 when the canvas has not
// been measured yet — which is the case while build runs, because the window is
// shown afterwards.
func (g *gui) canvasWidth() int {
	if g.win == nil {
		return 0
	}
	canvas := g.win.Canvas()
	if canvas == nil {
		return 0
	}

	return int(canvas.Size().Width)
}

// --- rendering --------------------------------------------------------

func (g *gui) setStatus(text string) {
	fyne.Do(func() { g.status.SetText(text) })
}

// addMessage appends a line to the transcript and scrolls to it.
//
// The list reads g.messages by index, so one Refresh is enough: the row count,
// the rows' text and the drop that keeps the log at maxMessages all follow from
// the slice.
func (g *gui) addMessage(m chatMessage) {
	fyne.Do(func() {
		g.messages = appendMessage(g.messages, m)
		g.chatList.Refresh()
		g.chatList.ScrollToBottom()
	})
}

// updateChatRow paints one row of the transcript.
//
// A wrapping label's height depends on the width it is given, and the list asks
// for a row's height before laying that row out at its width. Measuring at the
// width the list just supplied and handing the height back is the supported way
// to let a row be taller than the template — List.SetItemHeight exists for
// exactly this, and it stops as soon as the height stops changing.
//
// A row is a container: the label says what happened, the picture is an inline
// image on a file row, and the button is the Save action a received file
// offers. The picture and the button are laid out for every row and shown only
// when that row has one, so a text row's height stays what the label needs.
func (g *gui) updateChatRow(id widget.ListItemID, obj fyne.CanvasObject) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	row, ok := obj.(*fyne.Container)
	if !ok || len(row.Objects) < 3 {
		return
	}

	label, _ := row.Objects[0].(*widget.Label)
	pic, _ := row.Objects[1].(*canvas.Image)
	save, _ := row.Objects[2].(*widget.Button)

	if label == nil || pic == nil || save == nil {
		return
	}

	m := g.messages[id]
	label.SetText(m.line())
	g.fillFileRow(m, pic, save)

	width := row.Size().Width
	if g.chatList == nil || width <= 0 {
		return
	}

	row.Resize(fyne.NewSize(width, row.MinSize().Height))
	g.chatList.SetItemHeight(id, row.MinSize().Height)
}

// selectChatRow copies the message that was tapped, or opens a picture.
//
// This is the copy gesture a chat app teaches: tap the line and the line is on
// the clipboard, instead of asking a finger to drag a selection handle through
// a tall, wrapping label. A picture row is the exception — tapping it opens the
// file the way tapping an image everywhere else does.
func (g *gui) selectChatRow(id widget.ListItemID) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	message := g.messages[id]

	g.chatList.Unselect(id)

	if message.file != nil && message.file.inline && message.file.path != "" {
		g.showImage(message.file.path)

		return
	}

	g.copyToClipboard(message.line())
}

// copyAll copies the whole transcript.
func (g *gui) copyAll() {
	g.copyToClipboard(transcript(g.messages))
}

// copyToClipboard puts text on the clipboard and confirms it on screen.
func (g *gui) copyToClipboard(text string) {
	if g.app == nil || text == "" {
		return
	}

	g.app.Clipboard().SetContent(text)

	fyne.Do(func() { g.copyHint.SetText("Copied") })
	time.AfterFunc(copyHintDuration, func() {
		fyne.Do(func() { g.copyHint.SetText("") })
	})
}

// showError reports a failure in a dialog whose message can be copied.
//
// A popup used to be an inert label: the one message a user is most likely to
// need to paste somewhere was the one they could only retype. The text is
// selectable now, and a Copy button takes all of it at once.
func (g *gui) showError(err error) {
	if err == nil {
		return
	}

	fyne.Do(func() { g.showCopyable("Error", err.Error()) })
}

// addBeacon records a device heard now. A beacon heard twice is one device
// whose last-seen time moves.
func (g *gui) addBeacon(beacon discovery.Beacon) {
	g.beacons = upsertDevice(g.beacons, beacon, time.Now())
	g.beaconList.Refresh()
}

// pruneBeacons drops the devices that went quiet. It runs on a timer, so a
// host that was stopped leaves the list even when no scan is pressed.
func (g *gui) pruneBeacons() {
	if g.beaconList == nil {
		return
	}

	before := len(g.beacons)
	g.beacons = pruneDevices(g.beacons, time.Now())

	if len(g.beacons) != before {
		g.beaconList.Refresh()
	}
}

// pruneLoop sweeps the Devices list until the app closes.
func (g *gui) pruneLoop() {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()

	for range ticker.C {
		fyne.Do(g.pruneBeacons)
	}
}

// startDiscovery listens for beacons announced by other hosts.
func (g *gui) startDiscovery() {
	defer func() {
		if r := recover(); r != nil {
			// Ignore discovery panics on mobile
		}
	}()
	g.listener = discovery.NewListener()
	if g.listener != nil {
		g.listener.OnFound(func(beacon discovery.Beacon) {
			// This device's own announcement is dropped here, off the UI
			// thread: the operating system can loop a broadcast back to the
			// sender, and a device is not a peer to itself. The check reads the
			// interfaces, so it stays out of the render path.
			if isSelf(beacon.Addr, localAddrs()) {
				return
			}

			fyne.Do(func() {
				g.addBeacon(beacon)
				g.setStatus("Discovered " + beacon.Name)
			})
		})
		g.listener.Start()
	}
}

// --- settings ---------------------------------------------------------

func (g *gui) port() int {
	return parsePort(g.portEntry.Text)
}

func (g *gui) subject() string {
	return subjectOrDefault(g.subjectEntry.Text, natscore.DefaultSubject)
}

func (g *gui) data() *appdata.Data {
	data := appdata.New(g.port())
	data.Name = strings.TrimSpace(g.nameEntry.Text)
	data.Host = strings.TrimSpace(g.bindEntry.Text)

	// Pairing pins a peer by name, so an empty field still needs one.
	data.Name = data.ResolveName()

	return data
}

// tls returns the TLS settings. The certificate paths come from configDir, so
// the host writes where the app can actually write — on Android there is no
// $HOME to fall back on.
func (g *gui) tls() auth.TLS {
	dir := g.configDir()

	return auth.TLS{
		Enabled:     g.tlsCheck.Checked(),
		Insecure:    g.insecureCheck.Checked(),
		Fingerprint: strings.TrimSpace(g.fingerprintEntry.Text),
		CertFile:    filepath.Join(dir, "cert.pem"),
		KeyFile:     filepath.Join(dir, "key.pem"),
	}
}

func (g *gui) tokenFile() string {
	return filepath.Join(g.configDir(), "token")
}

// buildAbout lays out the About tab: which version this is, where it comes
// from, and — only when the button is pressed — whether there is a newer one.
//
// Nothing here polls. A start-up check would put a network call in the path of
// a window that has to open on a phone with no signal, and this tab exists to
// answer a question, not to chase one. The check runs on demand, in the
// background, and writes its answer on the tab it belongs to.
func (g *gui) buildAbout() fyne.CanvasObject {
	version := aboutVersion(Version, g.app.Metadata().Version)

	g.aboutCredential = selectableLabel("")
	g.aboutNote = selectableLabel("")

	// No Copy button beside the version: the version is a selectable label like
	// every other line here, so the button only repeated a selection the user
	// can already make.
	versionLine := container.NewHBox(
		widgets.SelectableHeading("cthrone"),
		selectableLabel(version),
	)

	repoBtn := widget.NewButtonWithIcon("Open", theme.InfoIcon(), g.openRepository)
	repoLine := container.NewBorder(nil, nil, nil, repoBtn, selectableLabel(RepositoryURL))

	g.updateBtn = widget.NewButtonWithIcon("Check for updates", theme.ViewRefreshIcon(), g.checkUpdates)

	note := selectableLabel("Nothing is downloaded and nothing is installed: the check only reports the version, and the release page does the rest.")

	body := container.NewVBox(
		versionLine,
		repoLine,
		widget.NewSeparator(),
		g.aboutCredential,
		widget.NewSeparator(),
		g.updateBtn,
		g.aboutNote,
		note,
	)

	// The credential line was filled in before this tab existed, so it is
	// brought up to date now rather than staying empty until the next rebuild.
	g.refreshCredential()

	return container.NewVScroll(container.NewPadded(body))
}

// openRepository opens the project's page in the browser.
//
// A link that does nothing is the failure this reports: `OpenURL` returns the
// reason a desktop has no browser or a phone has no handler, and a swallowed
// one leaves the user clicking a dead button.
func (g *gui) openRepository() {
	link, err := url.Parse(RepositoryURL)
	if err != nil {
		g.showError(err)

		return
	}

	if err := g.app.OpenURL(link); err != nil {
		g.showError(fmt.Errorf("could not open %s: %w", RepositoryURL, err))
	}
}

// checkUpdates asks GitHub for the newest release, off the UI goroutine.
//
// The answer is always written back through fyne.Do, including the failures: a
// button left disabled or a note left saying "Checking…" is the spinner that
// never resolves this tab is built to avoid.
func (g *gui) checkUpdates() {
	if g.updateBtn != nil {
		g.updateBtn.Disable()
	}

	g.setAboutNote("Checking…")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
		defer cancel()

		tag, err := latestReleaseTag(ctx, &http.Client{Timeout: updateCheckTimeout}, LatestReleaseURL)

		fyne.Do(func() {
			if g.updateBtn != nil {
				g.updateBtn.Enable()
			}

			if err != nil {
				g.setAboutNote("Could not check: " + err.Error() + ".")

				return
			}

			g.setAboutNote(updateText(aboutVersion(Version, g.app.Metadata().Version), tag))
		})
	}()
}

// setAboutNote writes the update check's answer on the About tab.
func (g *gui) setAboutNote(text string) {
	if g.aboutNote == nil {
		return
	}

	g.aboutNote.SetText(text)
}

// refreshCredential keeps the Setup tab saying which credential the next Host
// or Connect will speak, and offers to delete the token file while that file is
// the thing in charge.
//
// The button is hidden when a typed secret or the environment already wins:
// deleting the file would then change nothing the user could see, and a button
// that does nothing is worse than no button.
func (g *gui) refreshCredential() {
	if g.credential == nil || g.tokenEntry == nil {
		return
	}

	typed := strings.TrimSpace(g.tokenEntry.Text)
	envSet := os.Getenv(auth.EnvToken) != ""
	path := g.tokenFile()
	fromFile := secretFileHasValue(path)

	text := credentialText(typed, envSet, path, fromFile)
	g.credential.SetText(text)

	if g.aboutCredential != nil {
		g.aboutCredential.SetText(text)
	}

	if fromFile && typed == "" && !envSet {
		g.forgetSecret.Show()
	} else {
		g.forgetSecret.Hide()
	}
}

// forgetSecretFile deletes the token file after a confirmation, which is what
// puts this device back on key pairing.
//
// It asks first because the file is the only place that secret exists: a device
// that still holds it will not connect afterwards, and the deletion cannot be
// undone from this screen.
func (g *gui) forgetSecretFile() {
	path := g.tokenFile()

	g.showCopyableConfirm(
		"Stop using the shared secret",
		"The shared secret in\n"+path+"\nwill be deleted.\n\nThis device will pair by its key again. A device that still holds the secret has to clear it too before the two connect.",
		"Delete",
		"Cancel",
		func(confirmed bool) {
			if !confirmed {
				return
			}

			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				g.showError(err)

				return
			}

			g.refreshCredential()
			g.setStatus("Deleted " + path + " — this device pairs by its key again.")
		},
	)
}

// pairingOptions names the files the app keeps its credential in.
//
// They live in configDir rather than at the CLI's default, because a phone has
// no ~/.config to rely on; on the desktop configDir is that same directory, so
// the GUI and the CLI share one identity and one pin store.
func (g *gui) pairingOptions() pairing.Options {
	dir := g.configDir()

	return pairing.Options{
		FlagToken: strings.TrimSpace(g.tokenEntry.Text),
		TokenFile: g.tokenFile(),
		KeyFile:   filepath.Join(dir, "identity.key"),
		PinsFile:  filepath.Join(dir, "peers.json"),
	}
}

// configDir returns the directory the app keeps its token and TLS material in.
// The desktop shares it with the CLI; the phone uses the app's own storage.
func (g *gui) configDir() string {
	storage := ""
	if uri := g.app.Storage().RootURI(); uri != nil {
		storage = uri.Path()
	}

	return resolveConfigDir(g.mobile, storage)
}
