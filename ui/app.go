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
	bluetoothcore "github.com/fiwon123/cthrone/internal/core/bluetooth"
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

// openBluetoothSession and openBluetoothServer are the radio's two entry points.
// They are variables so a test can drive Connect and Host without an adapter:
// there is no radio in a test, and what is being checked is the window's side of
// the call — which target it names, what it shows, and how it stops.
var (
	openBluetoothSession = bluetoothcore.OpenSession
	openBluetoothServer  = bluetoothcore.OpenServer
)

// copyHintDuration is how long the transcript's "Copied" confirmation stays on
// screen. A tap that shows nothing reads as a tap that did nothing.
const copyHintDuration = 2 * time.Second

// copyHintMargin keeps the "Copied" popup off the bottom edge of the window.
const copyHintMargin = 48

// bluetoothScanTimeout is how long a radio scan listens. It matches the value
// the terminal scan uses: a device that was slow to advertise is exactly the one
// a shorter window would miss.
const bluetoothScanTimeout = 10 * time.Second

// liveServer is the part of a host the GUI needs. Keeping it an interface means
// the UI does not import the handler package, and the two transports that can
// host — the websocket listener and the Bluetooth one — are stopped the same
// way. Addr is not part of it: the radio has no address, and the address the
// user needs is already on screen from thisDeviceText.
type liveServer interface {
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

	// hostBtn and connectBtn are kept so they can be disabled while this device
	// is already hosting or connected. A second Host binds a port the first one
	// is holding, and the failure reads as "hosting is broken" rather than
	// "one at a time".
	hostBtn    *widgets.CopyButton
	connectBtn *widgets.CopyButton

	// setup layout: the form is rebuilt whenever the mode changes, because a
	// Fyne Form always renders a row for every item it holds.
	moreOptions bool
	moreBtn     *widgets.CopyButton
	formHolder  *fyne.Container
	transport   *widget.Select
	hint        *widget.Label

	// thisDevice is the read-only line that shows a hosting device its own
	// address. It is hidden until this device hosts: the Devices list never
	// lists this device, so this is where its address lives.
	thisDevice *widget.Label

	// credential is the read-only line that names the credential in force and
	// where it came from — this device's key, or a shared secret typed here or
	// set in the environment. It also reports a token file an older version
	// left behind: that file decides nothing now, and a user who is told so is
	// the one who can stop looking for the cause in the wrong place.
	credential *widget.Label

	// forgetSecret deletes the token file an older version read. It decides
	// nothing now, so the button is an offer to remove a leftover that used to
	// decide the model, not a switch.
	forgetSecret *widgets.CopyButton

	// about is the read-only tab: which build this is, where it comes from,
	// and — on a press, never on a start — whether there is a newer release.
	// aboutCredential repeats the Setup tab's line, because "which model am I
	// in?" belongs on the screen a bug report is written from.
	aboutCredential *widget.Label
	aboutNote       *widget.Label
	updateBtn       *widgets.CopyButton

	// chat
	tabs      *widgets.Tabs
	status    *widget.Label
	chatList  *widget.List
	entry     *widget.Entry
	sendBtn   *widgets.CopyButton
	stopBtn   *widgets.CopyButton
	attachBtn *widgets.CopyButton
	copyHint  *widget.Label
	messages  []chatMessage

	// copyPopup is the "Copied" hint a label copy raises, and copyTimer takes
	// it away again. The chat tab has its own line for the transcript's copy
	// buttons; a button on Setup, Devices, or About has no such line, so the
	// confirmation has to float above whichever tab is open.
	copyPopup *widget.PopUp
	copyTimer *time.Timer

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
	scanBtn    *widgets.CopyButton
	listHint   *widget.Label
	listener   *discovery.Listener

	mu     sync.Mutex
	sess   session.Session
	server liveServer

	// starting and stopping guard the handover of the transport. While either
	// is set, Host and Connect are disabled: a start that is still binding and a
	// stop that is still closing the listener both own the port, and a second
	// press would either bind it twice or meet it before it was released.
	starting bool
	stopping bool

	// startSeq identifies the Host/Connect attempt in flight. A Stop bumps it,
	// so an attempt that opens a host after the user already stopped is closed
	// instead of being installed as the live one.
	startSeq int
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

	// Closing the window is a stop. The desktop process exits anyway, but the
	// phone can outlive its window: a listener left bound there makes the next
	// Host fail with "address already in use".
	w.SetCloseIntercept(func() {
		g.shutdown()
		w.Close()
	})

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

	// The close intercept may not run on every platform — a phone that kills
	// the window rather than closing it — so the same release happens here.
	g.shutdown()

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
	g.copyHint = g.label("")

	copyLabel := "Copy all"
	if g.lay.Stack {
		// The icon is enough where width is scarce.
		copyLabel = ""
	}

	copyBtn := widgets.NewButtonWithIcon("Copy all", theme.ContentCopyIcon(), g.copyAll, g.copyLabel)
	copyBtn.SetText(copyLabel)

	// The Files action lives beside the transcript rather than in Setup: it is
	// about what happens to a received file, which is a chat concern, and Setup
	// is the connection form.
	filesLabel := "Files"
	if g.lay.Stack {
		filesLabel = ""
	}

	filesBtn := widgets.NewButtonWithIcon("Files", theme.FolderIcon(), g.showFiles, g.copyLabel)
	filesBtn.SetText(filesLabel)

	header := container.NewHBox()
	if g.mobile {
		header.Add(widget.NewLabelWithStyle("cthrone", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	}

	header.Add(layout.NewSpacer())
	header.Add(g.copyHint)
	header.Add(filesBtn)
	header.Add(copyBtn)

	g.status = g.label("Ready — open Setup to connect or host.")
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
			row := g.label("")

			pic := canvas.NewImageFromResource(nil)
			pic.FillMode = canvas.ImageFillContain
			pic.Hide()

			save := widgets.NewButtonWithIcon("Save as…", theme.DocumentSaveIcon(), nil, g.copyLabel)
			save.Hide()

			return container.NewVBox(row, pic, save)
		},
		g.updateChatRow,
	)
	g.chatList.OnSelected = g.selectChatRow

	// The composer is a chat field, not a form field: a message can be more
	// than one line, Enter starts a new line, and sending is a deliberate
	// action. Fyne already implements that half for a multi-line entry — it
	// stores Enter as a newline and calls OnSubmitted on Shift+Enter — so the
	// only thing left to wire is the same send the button calls.
	//
	// A word-wrapped body and a small visible-row count together keep the
	// composer's height fixed: a long paragraph scrolls inside the field
	// instead of pushing the transcript off the screen.
	g.entry = widget.NewMultiLineEntry()
	g.entry.Wrapping = fyne.TextWrapWord
	g.entry.SetMinRowsVisible(composerRowCount(g.lay.Stack))
	g.entry.SetPlaceHolder(composerHint(g.lay.Stack))
	g.entry.OnSubmitted = func(text string) { g.send(text) }

	g.sendBtn = widgets.NewButtonWithIcon("Send", theme.MailSendIcon(), func() { g.send(g.entry.Text) }, g.copyLabel)
	g.sendBtn.Disable()

	g.stopBtn = widgets.NewButtonWithIcon("Stop", theme.MediaStopIcon(), g.stop, g.copyLabel)
	g.stopBtn.Disable()

	attachLabel := "Attach"
	if g.lay.Stack {
		// The paperclip says it on a phone, where the row has no width to spare.
		attachLabel = ""
	}

	g.attachBtn = widgets.NewButtonWithIcon("Attach", theme.MailAttachmentIcon(), g.pickFile, g.copyLabel)
	g.attachBtn.SetText(attachLabel)
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
	g.tokenEntry.SetPlaceHolder("empty: pair by key, with nothing to copy")
	g.tokenEntry.OnChanged = func(string) { g.refreshCredential() }

	g.fingerprintEntry = widget.NewEntry()
	g.fingerprintEntry.SetPlaceHolder("certificate fingerprint (optional: pinned on first contact)")

	g.subjectEntry = widget.NewEntry()
	g.subjectEntry.SetText(natscore.DefaultSubject)

	g.natsURLEntry = widget.NewEntry()
	g.natsURLEntry.SetText(natscore.DefaultBrokerURL)

	g.tlsCheck = widgets.NewCheckRow("Encrypt with TLS", func(bool) { g.rebuildSetupForm() }, !g.mobile)
	// Encrypted is the default on the CLI, so the app starts the same way: a
	// phone that has to find a switch before it is protected is not protected.
	g.tlsCheck.SetChecked(true)
	g.insecureCheck = widgets.NewCheckRow("Skip the certificate check (insecure)", nil, !g.mobile)

	g.moreBtn = widgets.NewButton("More options", func() {
		g.moreOptions = !g.moreOptions
		g.rebuildSetupForm()
	}, g.copyLabel)

	// The transport select is the one control for the carrier: the three are
	// exclusive, so three checkboxes would be three ways to disagree. It is
	// built from transportNames over this build's capability, so a binary
	// without the radio does not offer it at all.
	g.transport = widget.NewSelect(transportNames(bluetoothSupported()), func(string) {
		g.rebuildSetupForm()
		g.refreshDeviceList()
	})
	g.transport.SetSelected(labelWebsocket)

	// The form is rebuilt into this stack: a Fyne form has no way to drop a
	// single row, so the visible subset becomes a fresh form instead.
	g.formHolder = container.NewStack()

	g.hostBtn = widgets.NewButtonWithIcon("Host", theme.MediaPlayIcon(), g.host, g.copyLabel)
	g.connectBtn = widgets.NewButtonWithIcon("Connect", theme.LoginIcon(), g.connect, g.copyLabel)

	actions := container.NewGridWithColumns(2, g.hostBtn, g.connectBtn)

	g.hint = g.label("")

	g.thisDevice = g.label("")
	g.thisDevice.Hide()

	g.credential = g.label("")

	g.forgetSecret = widgets.NewButtonWithIcon("Delete the old token file", theme.ContentClearIcon(), g.forgetSecretFile, g.copyLabel)
	g.forgetSecret.Hide()

	setup := container.NewVScroll(container.NewPadded(container.NewVBox(
		g.heading("Quick Setup"),
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
	g.scanBtn = widgets.NewButtonWithIcon("Scan network", theme.ViewRefreshIcon(), g.scan, g.copyLabel)

	// The list shows the rows of the carrier in force, not both at once: a
	// Bluetooth address is not something the websocket transport can dial, and
	// the other way round. rows re-reads that view on every refresh, so a scan
	// of one transport never leaves the other's rows in the way.
	g.beaconList = widget.NewList(
		func() int { return len(g.visibleBeacons()) },
		func() fyne.CanvasObject { return widget.NewLabel("") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			rows := g.visibleBeacons()
			if id < 0 || id >= len(rows) {
				return
			}

			if lbl, ok := obj.(*widget.Label); ok {
				lbl.SetText(deviceLabel(rows[id].Beacon))
			}
		},
	)
	g.beaconList.OnSelected = g.selectBeacon

	// The beacon hint wraps on a phone, where the button and the sentence do
	// not share a line without the sentence running off the screen.
	var beaconHeader fyne.CanvasObject
	g.listHint = g.label(scanHint(transportWebsocket))
	if g.lay.Stack {
		beaconHeader = container.NewVBox(g.scanBtn, g.listHint)
	} else {
		beaconHeader = container.NewHBox(g.scanBtn, g.listHint)
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

	g.tabs = widgets.NewTabs(g.lay.Stack, g.copyLabel,
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

	mode := g.mode()
	view := setupVisibility(mode, g.tlsCheck.Checked(), g.moreOptions)

	// The row that dials something names a different thing per transport, so
	// its placeholder says which: an address for a websocket, the first device
	// in range for the radio.
	switch mode {
	case transportBluetooth:
		g.hostEntry.SetPlaceHolder("device name or address (empty: the first in range)")
	default:
		g.hostEntry.SetPlaceHolder("192.168.1.100")
	}

	rows := []widgets.Row{
		{Caption: "Device name", Field: g.nameEntry},
		{Caption: labelConnectTo, Field: g.hostEntry},
	}
	// The radio has no port and no socket to bind: both rows would be settings
	// the transport cannot use.
	if view.Port {
		rows = append(rows, widgets.Row{Caption: "Port", Field: g.portEntry})
	}

	rows = append(rows, widgets.Row{Caption: "Transport", Field: g.transport})
	if view.Options {
		rows = append(rows, widgets.Row{Caption: "Options", Field: g.moreBtn})
	}
	if view.SharedSecret {
		rows = append(rows, widgets.Row{Caption: "Shared secret", Field: g.tokenEntry})
	}
	if view.Fingerprint {
		rows = append(rows, widgets.Row{Caption: "Fingerprint", Field: g.fingerprintEntry})
	}
	if view.NATSBroker {
		rows = append(rows, widgets.Row{Caption: "NATS URL", Field: g.natsURLEntry})
	}
	if view.NATSSubject {
		rows = append(rows, widgets.Row{Caption: "NATS subject", Field: g.subjectEntry})
	}
	if view.BindAddress {
		rows = append(rows, widgets.Row{Caption: "Bind address", Field: g.bindEntry})
	}

	// A check row hides its caption with the box, so the whole row goes.
	if view.Insecure {
		g.insecureCheck.Show()
	} else {
		g.insecureCheck.Hide()
	}

	if g.moreOptions {
		g.moreBtn.SetText("Fewer options")
		g.moreBtn.SetCopyText("Fewer options")
	} else {
		g.moreBtn.SetText("More options")
		g.moreBtn.SetCopyText("More options")
	}

	g.hint.SetText(setupHint(mode))

	g.formHolder.Objects = []fyne.CanvasObject{widgets.Form(g.lay.Stack, !g.mobile, rows)}
	g.formHolder.Refresh()

	g.refreshCredential()
}

// --- actions ----------------------------------------------------------

// beginStart reserves the transport for a Host or Connect attempt. It reports
// the sequence number that identifies the attempt, and false when one is
// already running or a host or a session already holds it. Both buttons are
// disabled while it is reserved, so a second press cannot bind the same port
// twice — the failure a user reads as "hosting is broken".
func (g *gui) beginStart() (int, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.starting || g.stopping || g.sess != nil || g.server != nil {
		return 0, false
	}

	g.startSeq++
	g.starting = true

	return g.startSeq, true
}

// finishStart ends attempt seq and, when it is still the current one, runs
// install under the same lock — so the state it publishes cannot be seen
// half-made, and a press that arrives in between is refused rather than
// starting a second transport. false means Stop arrived first: whatever the
// attempt opened must be closed instead of installed.
func (g *gui) finishStart(seq int, install func()) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.starting = false

	if g.startSeq != seq {
		return false
	}

	if install != nil {
		install()
	}

	return true
}

// connect joins a host (websocket), a broker (NATS), or a Bluetooth device.
func (g *gui) connect() {
	host := strings.TrimSpace(g.hostEntry.Text)
	mode := g.mode()

	// Only the websocket has nothing to find on its own: a Bluetooth connect
	// with no device named takes the first cthrone device in range, which is
	// what an empty field means there.
	if mode == transportWebsocket && host == "" {
		g.showError(errors.New("enter the host address to connect to"))

		return
	}

	seq, ok := g.beginStart()
	if !ok {
		return
	}

	data := g.data()
	tlsConf := g.tls()
	opts := g.pairingOptions()
	broker := g.natsURLEntry.Text
	subject := g.subject()

	g.setStatus("Connecting…")
	g.tabs.SelectIndex(0)
	g.updateButtons()

	go func() {
		var (
			sess session.Session
			err  error
		)

		switch mode {
		case transportNATS:
			sess, err = natscore.OpenSession([]string{subject}, data, opts, broker, tlsConf)

		case transportBluetooth:
			sess, err = openBluetoothSession(targetArg(host), data, opts)

		default:
			sess, err = websocketcore.Dial([]string{host}, data, opts, tlsConf)
		}

		if !g.finishStart(seq, func() { g.sess = sess }) {
			// Stop arrived while the dial was in flight: a session nobody asked
			// for is closed rather than attached, and its failure is not the
			// user's to read — they stopped before it arrived.
			if sess != nil {
				sess.Close("cancelled")
			}

			return
		}

		if err != nil {
			g.setStatus("Connection failed")
			g.showError(err)
			fyne.Do(g.updateButtons)

			return
		}

		switch mode {
		case transportNATS:
			g.setStatus("Connected through NATS on " + broker)
		case transportBluetooth:
			g.setStatus("Connected over Bluetooth — chatting")
		default:
			g.setStatus("Connected to " + host + " — chatting")
		}

		g.noteFirstContact(sess)
		g.attach(sess)
	}()
}

// targetArg is what a Bluetooth connect was pointed at: the named device, or
// nothing, which the transport reads as "the first cthrone device in range".
func targetArg(host string) []string {
	if host == "" {
		return nil
	}

	return []string{host}
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

// host starts a host (websocket), joins the broker as a peer (NATS), or serves
// a room over the radio (Bluetooth).
func (g *gui) host() {
	seq, ok := g.beginStart()
	if !ok {
		return
	}

	data := g.data()
	tlsConf := g.tls()
	opts := g.pairingOptions()
	broker := g.natsURLEntry.Text
	subject := g.subject()
	mode := g.mode()
	typedToken := strings.TrimSpace(g.tokenEntry.Text) != ""

	// This device's own address, as the other device's scan will find it. The
	// default route picks the network the device is actually on, so a docker
	// bridge or a VPN adapter is not advertised as a way in. Both are read
	// before the goroutine so no interface is touched off the UI thread, and
	// they are only meaningful for the websocket host: the NATS path has a
	// broker instead of an address, and the radio has neither.
	own := ""
	hosting := "Hosting — waiting for connection"

	if mode != transportNATS {
		route, _ := defaultRouteIP()
		addrs := discovery.AdvertisedAddrs(localAddrs(), route)
		own = thisDeviceText(addrs, data.Port)
		if addresses := deviceAddresses(addrs, data.Port); addresses != "" {
			hosting = "Hosting on " + addresses + " — waiting for connection"
		}
	}

	g.setStatus("Starting…")
	g.updateButtons()

	go func() {
		switch mode {
		case transportNATS:
			sess, err := natscore.OpenSession([]string{subject}, data, opts, broker, tlsConf)
			if err != nil {
				if !g.finishStart(seq, nil) {
					return
				}

				g.setStatus("Host failed")
				g.showError(err)
				fyne.Do(g.updateButtons)

				return
			}

			if !g.finishStart(seq, func() { g.sess = sess }) {
				sess.Close("cancelled")

				return
			}

			g.setStatus("Hosting through NATS on " + broker)
			g.attach(sess)
			g.tabs.SelectIndex(0)

			return

		case transportBluetooth:
			g.hostBluetooth(seq, data, opts)
		default:
			g.hostWebsocket(seq, data, tlsConf, opts, own, hosting, typedToken)
		}
	}()
}

// hostBluetooth serves a room over the radio.
//
// There is no address to show and no certificate to pin: the other device finds
// this one by scanning the radio, and the two screens are compared by
// fingerprint or by the six-digit code the host asks on. The dialog carries the
// fingerprint for exactly that reason — it is the only screen the user has.
func (g *gui) hostBluetooth(seq int, data *appdata.Data, opts pairing.Options) {
	handle, err := openBluetoothServer(data, opts, func(sess session.Session) {
		g.setStatus("A device joined over Bluetooth — chatting")
		g.attach(sess)
	}, g.confirmFirstContact)
	if err != nil {
		if !g.finishStart(seq, nil) {
			return
		}

		g.setStatus("Host failed")
		g.showError(err)
		fyne.Do(g.updateButtons)

		return
	}

	if !g.finishStart(seq, func() { g.server = handle }) {
		_ = handle.Stop()
		fyne.Do(g.updateButtons)

		return
	}

	fyne.Do(g.updateButtons)
	g.setStatus("Hosting over Bluetooth — one device at a time")
	g.tabs.SelectIndex(0)
	g.offerPairing("", handle.DeviceFingerprint, "", "", false)
}

// hostWebsocket serves the websocket room: it binds the port, shows this
// device's address, and prints what the other device needs to find it.
func (g *gui) hostWebsocket(seq int, data *appdata.Data, tlsConf auth.TLS, opts pairing.Options, own, hosting string, typedToken bool) {
	handle, err := websocketcore.OpenServer(data, opts, tlsConf, func(sess session.Session) {
		g.setStatus("A device joined — chatting")
		g.attach(sess)
	}, g.confirmFirstContact)
	if err != nil {
		if !g.finishStart(seq, nil) {
			return
		}

		g.setStatus("Host failed")
		g.showError(err)
		fyne.Do(g.updateButtons)

		return
	}

	// The listener is bound already. A Stop that arrived while it was
	// binding released the port it was told about, not this one, so this
	// server closes itself: otherwise the app would be running a host with
	// nobody holding the button that stops it.
	if !g.finishStart(seq, func() { g.server = handle }) {
		_ = handle.Stop()
		fyne.Do(g.updateButtons)

		return
	}

	fyne.Do(g.updateButtons)
	g.showThisDevice(own)
	g.setStatus(hosting)
	g.tabs.SelectIndex(0)
	g.offerPairing(handle.Token, handle.DeviceFingerprint, handle.Fingerprint, own, !typedToken)
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

	// A Start that is still in flight is cancelled: its server closes itself
	// when it opens instead of becoming the live one. Until this stop has
	// released the listener, neither Host nor Connect may run.
	g.startSeq++
	g.stopping = true
	g.mu.Unlock()

	g.finishReceive()
	g.hideThisDevice()
	g.setStatus("Stopping…")
	g.updateButtons()

	go func() {
		if sess != nil {
			sess.Close("closed from the app")
		}

		// Stop does not return before the listening port is closed; the buttons
		// come back only after it, so a Stop → Host cannot meet the old socket.
		if server != nil {
			_ = server.Stop()
		}

		g.mu.Lock()
		g.stopping = false
		g.mu.Unlock()

		fyne.Do(func() {
			g.updateButtons()
			g.setStatus("Disconnected")
		})
	}()
}

// shutdown releases the transport without touching the widgets, for the window
// closing: the port has to go back to the machine before the process does, and
// on the phone the process can outlive the window. It is synchronous because
// there is no event loop left to come back to.
func (g *gui) shutdown() {
	g.stopCopyHint()

	g.mu.Lock()
	sess, server := g.sess, g.server
	g.sess, g.server = nil, nil
	g.startSeq++
	g.mu.Unlock()

	if sess != nil {
		sess.Close("the app is closing")
	}

	if server != nil {
		_ = server.Stop()
	}
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

	// Text goes through the envelope, which is what keeps the one string the
	// receiver would misread — a message beginning with the magic — from
	// arriving as a damaged frame: Encode wraps that line in a text frame, and
	// the receiver unwraps it. For every other message Encode hands the bytes
	// back unchanged, so a peer that predates the envelope still reads it.
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

// scan finds devices on the carrier in force: the hosts answering on this
// network, or the cthrone devices in radio range.
//
// The devices already listed are re-checked too: a host that was stopped
// since it was heard fails the check and leaves before its TTL would have
// expired it, so pressing the button never keeps a dead address around.
func (g *gui) scan() {
	mode := g.mode()

	if mode == transportBluetooth {
		g.scanBluetooth(mode)

		return
	}

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
			g.refreshDeviceList()

			if len(found) == 0 {
				g.setStatus("No host answered on this network")

				return
			}

			g.setStatus(fmt.Sprintf("Found %d host(s) — tap one to connect", len(found)))
		})
	}()
}

// scanBluetooth walks the radio. There is no beacon to listen for on this
// transport and no port to probe, so the scan is the whole of discovery: what
// advertises the service is what can be dialled.
func (g *gui) scanBluetooth(mode transportMode) {
	g.scanBtn.Disable()
	g.setStatus("Scanning for Bluetooth devices — about ten metres…")

	go func() {
		found := 0

		err := bluetoothcore.ScanPeers(bluetoothScanTimeout, func(p bluetoothcore.Peer) {
			found++
			// The peer arrives on the scan's goroutine, so the list is touched
			// on the UI one.
			fyne.Do(func() { g.addPeer(p) })
		})

		fyne.Do(func() {
			g.scanBtn.Enable()

			if err != nil {
				g.setStatus("Bluetooth scan failed")
				g.showError(err)

				return
			}

			if found == 0 {
				g.setStatus("No cthrone device answered over Bluetooth")

				return
			}

			g.setStatus(fmt.Sprintf("Found %d Bluetooth device(s) — tap one to connect", found))
		})
	}()
}

// visibleBeacons is the rows the Devices list shows for the transport in force.
func (g *gui) visibleBeacons() []device {
	return visibleDevices(g.beacons, g.mode())
}

// refreshDeviceList re-reads the view and the hint after the carrier changed:
// the rows of the other transport are not peers the Connect button can reach.
func (g *gui) refreshDeviceList() {
	if g.beaconList == nil {
		return
	}

	if g.listHint != nil {
		g.listHint.SetText(scanHint(g.mode()))
	}

	if g.scanBtn != nil {
		g.scanBtn.SetText(scanButtonText(g.mode()))
		g.scanBtn.SetCopyText(scanButtonText(g.mode()))
	}

	g.beaconList.UnselectAll()
	g.beaconList.Refresh()
}

// addPeer records a device the radio found.
func (g *gui) addPeer(p bluetoothcore.Peer) {
	g.beacons = upsertPeer(g.beacons, p, time.Now())
	g.beaconList.Refresh()
}

// selectBeacon fills the host address from a discovered device.
func (g *gui) selectBeacon(id widget.ListItemID) {
	rows := g.visibleBeacons()
	if id < 0 || id >= len(rows) {
		return
	}

	beacon := rows[id]

	g.hostEntry.SetText(beacon.Addr)

	// A radio address has no port: the row offers the device, not an address on
	// a network, and the port field is not on screen in that mode anyway.
	if beacon.Port > 0 {
		g.portEntry.SetText(strconv.Itoa(beacon.Port))
	}

	g.setStatus("Selected " + deviceLabel(beacon.Beacon))
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

	// The close notice is the one line that has to name the peer after the
	// session is gone, so the name is captured here rather than read from a
	// session that may already be closed.
	peer := &peerName{}

	// A sealed session reports a peer the moment its handshake authenticates,
	// and a bus refusal that dropped one peer without ending the room. Both
	// belong on the status line: a silent drop looks like a peer that never
	// sent anything, and "connected" is not the same as "encrypted".
	if sealed, ok := sess.(*e2e.Conn); ok {
		sealed.OnPeer(func(p e2e.Peer) {
			peer.set(p.Name())

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
		peer.learn(m.From)

		// The sender string is the authenticated device name, and the address
		// is carried beside it: the transcript shows both, so a name shared by
		// two machines still says which one is talking.
		//
		// A payload is a chat message or a piece of a file: the envelope says
		// which, and text that predates it still arrives as text.
		g.receiveFrame(senderLabel(m.From, m.Addr), m.Payload)
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

			// A session the user ended is not a peer who left: Stop clears the
			// live session before it closes it, so only a close that arrives
			// while this session is still the live one leaves a line. That is
			// exactly the case a host has to be able to read afterwards.
			g.addMessage(chatMessage{from: "system", text: disconnectLine(peer.get(), reason)})
		}

		fyne.Do(g.updateButtons)
		g.setStatus("Disconnected: " + reason)
	})
}

// updateButtons enables what the current state can actually do. Send needs a
// session; Stop also makes sense for a host with nobody connected yet. Host and
// Connect change what this device is, and there can be only one of them — so
// they are off while a host, a session, or a start/stop still owns the port.
func (g *gui) updateButtons() {
	g.mu.Lock()
	hasSession := g.sess != nil
	hasServer := g.server != nil
	busy := g.starting || g.stopping
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

	if g.hostBtn == nil || g.connectBtn == nil {
		return
	}

	if busy || hasSession || hasServer {
		g.hostBtn.Disable()
		g.connectBtn.Disable()
	} else {
		g.hostBtn.Enable()
		g.connectBtn.Enable()
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
	save, _ := row.Objects[2].(*widgets.CopyButton)

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

// copyLabel is the onCopied hook every CopyButton carries.
//
// The widget has already put the label on the clipboard; this is the part that
// says so. It cannot use the chat header's line, because the button may be on
// Setup, Devices, or About, where that line is not on screen.
func (g *gui) copyLabel(string) {
	g.showCopied()
}

// showCopied floats a short "Copied" above whatever tab is open.
//
// A popup rather than a toast because Fyne has no toast, and it is non-modal:
// a single tap anywhere takes it away, so it never stands between the user and
// the next action. Repeated copies reuse the popup and restart its timer
// instead of stacking one hint per press.
func (g *gui) showCopied() {
	if g.win == nil || g.win.Canvas() == nil {
		return
	}

	fyne.Do(func() {
		if g.copyPopup == nil {
			hint := widget.NewLabel("Copied")
			hint.Alignment = fyne.TextAlignCenter
			g.copyPopup = widget.NewPopUp(hint, g.win.Canvas())
		}

		size := g.copyPopup.MinSize()
		canvasSize := g.win.Canvas().Size()
		g.copyPopup.ShowAtPosition(fyne.NewPos(
			fyne.Max(0, (canvasSize.Width-size.Width)/2),
			fyne.Max(0, canvasSize.Height-size.Height-copyHintMargin),
		))

		if g.copyTimer != nil {
			g.copyTimer.Stop()
		}

		g.copyTimer = time.AfterFunc(copyHintDuration, func() {
			fyne.Do(func() {
				if g.copyPopup != nil {
					g.copyPopup.Hide()
				}
			})
		})
	})
}

// stopCopyHint takes the hint down and stops its timer. It runs when the window
// closes and from the tests, so a timer that would fire into a canvas nobody is
// drawing is cancelled with the window it belongs to.
func (g *gui) stopCopyHint() {
	if g.copyTimer != nil {
		g.copyTimer.Stop()
		g.copyTimer = nil
	}

	if g.copyPopup != nil {
		g.copyPopup.Hide()
	}
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

// mode is the transport the select points at. It is the single reading of that
// control: the connect, host and scan paths all ask here rather than each
// deciding again from a checkbox.
func (g *gui) mode() transportMode {
	if g.transport == nil {
		return transportWebsocket
	}

	return transportOf(g.transport.Selected)
}

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

// tokenFile is the token file older versions read without being asked. This
// build never opens it: it is named on the Setup tab so a user can see that a
// leftover is there, and deleted by a press of the button beside that line.
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

	g.aboutCredential = g.label("")
	g.aboutNote = g.label("")

	// The name is the heading and the version sits under it, rather than the
	// two sharing one line: at phone width "cthrone v0.8.3" in a horizontal box
	// wraps mid-word or pushes the version off the edge, and a header that
	// changes shape with the window is the thing that looked broken.
	header := g.aboutHeader(version)

	repoBtn := widgets.NewButtonWithIcon("Open", theme.InfoIcon(), g.openRepository, g.copyLabel)
	repoLine := container.NewBorder(nil, nil, nil, repoBtn, g.label(RepositoryURL))

	g.updateBtn = widgets.NewButtonWithIcon("Check for updates", theme.ViewRefreshIcon(), g.checkUpdates, g.copyLabel)

	note := g.label("Nothing is downloaded and nothing is installed: the check only reports the version, and the release page does the rest.")

	body := container.NewVBox(
		header,
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

// aboutHeader is the app's name above the version it is running.
//
// It is a block rather than one line so a phone and a desktop draw the same
// header: side by side, a narrow window has to either wrap the name or push the
// version out of the pane.
func (g *gui) aboutHeader(version string) fyne.CanvasObject {
	return container.NewVBox(
		g.heading("cthrone"),
		g.label(version),
	)
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
// or Connect will speak, and offers to delete the token file an older version
// left behind.
//
// The button is shown whenever that file exists, whatever the credential in
// force: the file no longer decides anything, so it is litter that used to
// decide something, and a user who has just been told it is no longer read is
// the one who wants it gone.
func (g *gui) refreshCredential() {
	if g.credential == nil || g.tokenEntry == nil {
		return
	}

	typed := strings.TrimSpace(g.tokenEntry.Text)
	envSet := os.Getenv(auth.EnvToken) != ""
	path := g.tokenFile()
	legacy := secretFileExists(path)

	text := credentialText(typed, envSet, path, legacy)
	g.credential.SetText(text)

	if g.aboutCredential != nil {
		g.aboutCredential.SetText(text)
	}

	if legacy {
		g.forgetSecret.Show()
	} else {
		g.forgetSecret.Hide()
	}
}

// forgetSecretFile deletes the token file an older version left behind.
//
// The file is not read by this build, so deleting it changes no connection by
// itself; it removes the last trace of a secret that stopped applying, which is
// what stops the next person from looking for the cause in the wrong place. A
// typed secret or the environment is untouched — this deletes a file, nothing
// else.
func (g *gui) forgetSecretFile() {
	path := g.tokenFile()

	g.showCopyableConfirm(
		"Delete the old token file",
		"The file\n"+path+"\nis no longer read by this version.\n\nDeleting it changes no connection here: this device pairs by its key, and a shared secret has to be typed on the Setup tab. Delete the leftover file?",
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
			g.setStatus("Deleted " + path + " — this device pairs by its key.")
		},
	)
}

// pairingOptions names the files the app keeps its credential in.
//
// They live in configDir rather than at the CLI's default, because a phone has
// no ~/.config to rely on; on the desktop configDir is that same directory, so
// the GUI and the CLI share one identity and one pin store.
//
// No token file is passed: the app reads a shared secret only from the field
// on the Setup tab, so a file left behind by an older version cannot move the
// device into the secret model on its own. g.tokenFile is named on screen and
// offered for deletion instead.
func (g *gui) pairingOptions() pairing.Options {
	dir := g.configDir()

	return pairing.Options{
		FlagToken: strings.TrimSpace(g.tokenEntry.Text),
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
