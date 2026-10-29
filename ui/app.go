//go:build gui

// Package ui is the Fyne GUI.
//
// It is built with -tags gui: without that tag ui.Run is the stub in
// app_nogui.go and the CLI does not need the GUI toolkit. The desktop app and
// the Android/iOS app share this file; the only differences are the entry
// point (cmd/ui for the desktop, mobile/main.go for the APK) and the touch
// adjustments made when CurrentDevice().IsMobile() is true.
package ui

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/internal/auth"
	natscore "github.com/fiwon123/cthrone/internal/core/nats"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	appdata "github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/session"
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

	// setup
	portEntry        *widget.Entry
	bindEntry        *widget.Entry
	nameEntry        *widget.Entry
	hostEntry        *widget.Entry
	tokenEntry       *widget.Entry
	fingerprintEntry *widget.Entry
	subjectEntry     *widget.Entry
	natsURLEntry     *widget.Entry
	tlsCheck         *widget.Check
	insecureCheck    *widget.Check
	natsCheck        *widget.Check
	anonymousCheck   *widget.Check

	// setup layout: the form is rebuilt whenever the mode changes, because a
	// Fyne Form always renders a label for every item it holds.
	moreOptions bool
	moreBtn     *widget.Button
	securityRow *fyne.Container
	formHolder  *fyne.Container
	transport   *widget.Select
	hint        *widget.Label

	// chat
	tabs     *container.AppTabs
	status   *widget.Label
	chatList *widget.List
	entry    *widget.Entry
	sendBtn  *widget.Button
	stopBtn  *widget.Button
	copyHint *widget.Label
	messages []chatMessage

	// devices
	beacons    []discovery.Beacon
	beaconList *widget.List
	scanBtn    *widget.Button
	listener   *discovery.Listener

	mu     sync.Mutex
	sess   session.Session
	server liveServer
}

// Run starts the Fyne GUI.
func Run() {
	defer func() {
		if r := recover(); r != nil {
			// avoid crash
		}
	}()
	var mobile bool
	func() {
		defer func() {
			recover()
		}()
		if dev := fyne.CurrentDevice(); dev != nil {
			mobile = dev.IsMobile()
		}
	}()
	var a fyne.App
	var w fyne.Window
	func() {
		defer func() {
			recover()
		}()
		a = fyneapp.NewWithID("com.cthrone.app")
		if a != nil {
			if ic := appIcon(); ic != nil {
				a.SetIcon(ic)
			}
		}
		if a != nil {
			w = a.NewWindow("cthrone")
			if w != nil {
				if ic := appIcon(); ic != nil {
					w.SetIcon(ic)
				}
			}
		}
	}()
	if a == nil || w == nil {
		return
	}
	g := &gui{app: a, win: w, mobile: mobile}
	func() {
		defer func() {
			recover()
		}()
		g.build()
	}()
	if !mobile {
		func() {
			defer func() {
				recover()
			}()
			g.startDiscovery()
		}()
		func() {
			defer func() {
				recover()
			}()
			w.Resize(fyne.NewSize(900, 640))
		}()
	} else {
		// Skip discovery on mobile to avoid startup crashes
		func() {
			defer func() {
				recover()
			}()
		}()
	}
	func() {
		defer func() {
			recover()
		}()
		w.ShowAndRun()
	}()
}

// build creates every widget and lays the window out.
func (g *gui) build() {
	// --- chat ---------------------------------------------------------
	// The desktop window already says "cthrone" in its title bar, so the header
	// carries only what a title bar cannot: the name on a phone, which has no
	// title bar to show it in, the copy confirmation, and the copy-all action.
	g.copyHint = widget.NewLabel("")

	copyLabel := "Copy all"
	if g.mobile {
		// The icon is enough where width is scarce.
		copyLabel = ""
	}

	copyBtn := widget.NewButtonWithIcon(copyLabel, theme.ContentCopyIcon(), g.copyAll)

	header := container.NewHBox()
	if g.mobile {
		header.Add(widget.NewLabelWithStyle("cthrone", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}))
	}

	header.Add(layout.NewSpacer())
	header.Add(g.copyHint)
	header.Add(copyBtn)

	g.status = widget.NewLabel("Ready — open Setup to connect or host.")
	g.status.TextStyle = fyne.TextStyle{Bold: true}
	g.status.Wrapping = fyne.TextWrapWord

	// One row per message, not one label holding the whole log: appending then
	// costs a row instead of a re-wrap of every line, and a row is a tap target
	// a finger can hit.
	g.chatList = widget.NewList(
		func() int { return len(g.messages) },
		func() fyne.CanvasObject {
			row := widget.NewLabel("")
			row.Wrapping = fyne.TextWrapWord

			return row
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

	var input fyne.CanvasObject
	if g.mobile {
		// Touch: a full-width field, and two large targets below it.
		input = container.NewVBox(g.entry, container.NewGridWithColumns(3, g.stopBtn, g.sendBtn, copyBtn))
	} else {
		input = container.NewBorder(nil, nil, nil, container.NewHBox(g.stopBtn, copyBtn, g.sendBtn), g.entry)
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
	g.tokenEntry.SetPlaceHolder("empty uses the token file")

	g.fingerprintEntry = widget.NewEntry()
	g.fingerprintEntry.SetPlaceHolder("certificate fingerprint (needed with TLS)")

	g.subjectEntry = widget.NewEntry()
	g.subjectEntry.SetText(natscore.DefaultSubject)

	g.natsURLEntry = widget.NewEntry()
	g.natsURLEntry.SetText(natscore.DefaultBrokerURL)

	g.tlsCheck = widget.NewCheck("Encrypt with TLS", func(bool) { g.rebuildSetupForm() })
	g.insecureCheck = widget.NewCheck("Skip the certificate check (insecure)", nil)
	g.natsCheck = widget.NewCheck("Use NATS instead of websocket", nil)
	g.anonymousCheck = widget.NewCheck("Allow anonymous clients (no shared secret)", nil)
	g.anonymousCheck.SetChecked(true)

	g.moreBtn = widget.NewButton("More options", func() {
		g.moreOptions = !g.moreOptions
		g.rebuildSetupForm()
	})

	g.securityRow = container.NewHBox(g.anonymousCheck, g.moreBtn)

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

	g.hint = widget.NewLabel("")
	g.hint.Wrapping = fyne.TextWrapWord

	setup := container.NewVScroll(container.NewPadded(container.NewVBox(
		widget.NewLabelWithStyle("Quick Setup", fyne.TextAlignLeading, fyne.TextStyle{Bold: true}),
		g.tlsCheck,
		g.insecureCheck,
		g.formHolder,
		widget.NewSeparator(),
		actions,
		g.hint,
	)))

	g.rebuildSetupForm()

	// --- devices ------------------------------------------------------
	g.scanBtn = widget.NewButtonWithIcon("Scan network", theme.ViewRefreshIcon(), g.scan)

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

	devices := container.NewBorder(
		container.NewVBox(
			container.NewPadded(container.NewHBox(g.scanBtn, widget.NewLabel("UDP beacons on port 9999"))),
			widget.NewSeparator(),
		),
		nil, nil, nil,
		g.beaconList,
	)

	g.tabs = container.NewAppTabs(
		container.NewTabItemWithIcon("Chat", theme.MailComposeIcon(), chat),
		container.NewTabItemWithIcon("Setup", theme.SettingsIcon(), setup),
		container.NewTabItemWithIcon("Devices", theme.ComputerIcon(), devices),
	)

	g.win.SetContent(g.tabs)
}

// rebuildSetupForm lays out the rows the current configuration uses.
//
// Fyne's widget.Form renders a label for every FormItem it holds, and there is
// no public handle on that label, so hiding a field's entry leaves its label
// dangling on screen. The rows are therefore rebuilt from the visible subset:
// the entries themselves live on g, so the text already typed into them
// survives.
func (g *gui) rebuildSetupForm() {
	// The transport's OnSelected runs while build() is still wiring the tab.
	if g.formHolder == nil {
		return
	}

	useNATS := g.natsCheck.Checked
	view := setupVisibility(useNATS, g.tlsCheck.Checked, g.moreOptions)

	items := []*widget.FormItem{
		{Text: "Device name", Widget: g.nameEntry},
		{Text: "Host address", Widget: g.hostEntry},
		{Text: "Port", Widget: g.portEntry},
		{Text: "Transport", Widget: g.transport},
	}
	if view.Security {
		items = append(items, &widget.FormItem{Text: "Security", Widget: g.securityRow})
	}
	if view.SharedSecret {
		items = append(items, &widget.FormItem{Text: "Shared secret", Widget: g.tokenEntry})
	}
	if view.Fingerprint {
		items = append(items, &widget.FormItem{Text: "Fingerprint", Widget: g.fingerprintEntry})
	}
	if view.NATSBroker {
		items = append(items, &widget.FormItem{Text: "NATS URL", Widget: g.natsURLEntry})
	}
	if view.NATSSubject {
		items = append(items, &widget.FormItem{Text: "NATS subject", Widget: g.subjectEntry})
	}
	if view.BindAddress {
		items = append(items, &widget.FormItem{Text: "Bind address", Widget: g.bindEntry})
	}

	// A check carries its own label, so hiding the widget removes the text.
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
}

// --- actions ----------------------------------------------------------

// connect joins a host (websocket) or a broker (NATS).
func (g *gui) connect() {
	host := strings.TrimSpace(g.hostEntry.Text)
	useNATS := g.natsCheck.Checked

	if !useNATS && host == "" {
		dialog.ShowError(errors.New("enter the host address to connect to"), g.win)

		return
	}

	data := g.data()
	tlsConf := g.tls()
	token := g.tokenEntry.Text
	tokenFile := g.tokenFile()
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
			sess, err = natscore.OpenSession([]string{subject}, data, broker, tlsConf)
		} else {
			sess, err = websocketcore.Dial([]string{host}, data, token, tokenFile, tlsConf)
		}

		if err != nil {
			g.setStatus("Connection failed")
			g.showError(err)

			return
		}

		g.attach(sess)

		if useNATS {
			g.setStatus("Connected through NATS on " + broker)
		} else {
			g.setStatus("Connected to " + host + " — chatting")
		}
	}()
}

// host starts a host (websocket) or joins the broker as a peer (NATS).
func (g *gui) host() {
	data := g.data()
	tlsConf := g.tls()
	token := g.tokenEntry.Text
	tokenFile := g.tokenFile()
	broker := g.natsURLEntry.Text
	subject := g.subject()
	useNATS := g.natsCheck.Checked
	typedToken := strings.TrimSpace(token) != ""

	g.setStatus("Starting…")

	go func() {
		if useNATS {
			sess, err := natscore.OpenSession([]string{subject}, data, broker, tlsConf)
			if err != nil {
				g.setStatus("Host failed")
				g.showError(err)

				return
			}

			g.attach(sess)
			g.setStatus("Hosting through NATS on " + broker)
			g.tabs.SelectIndex(0)

			return
		}

		handle, err := websocketcore.OpenServer(data, token, tokenFile, tlsConf, func(sess session.Session) {
			g.attach(sess)
			g.setStatus("A device joined — chatting")
		})
		if err != nil {
			g.setStatus("Host failed")
			g.showError(err)

			return
		}

		g.mu.Lock()
		g.server = handle
		g.mu.Unlock()

		fyne.Do(g.updateButtons)
		g.setStatus("Hosting on " + handle.Addr() + " — waiting for connection")
		g.tabs.SelectIndex(0)
		g.offerPairing(handle.Token, handle.Fingerprint, !typedToken)
	}()
}

// offerPairing shows the values the other device needs. The terminal host
// prints them; a GUI has no terminal, and on Android there is no file to open,
// so this dialog is the only way to pair a phone that hosts.
func (g *gui) offerPairing(token, fingerprint string, showToken bool) {
	var message strings.Builder

	if showToken && token != "" {
		fmt.Fprintf(&message, "Shared secret:\n%s\n\n", token)
	}

	if fingerprint != "" {
		fmt.Fprintf(&message, "Certificate fingerprint:\n%s\n\n", fingerprint)
	}

	if message.Len() == 0 {
		return
	}

	message.WriteString("Enter the shared secret on the other device; with TLS, its fingerprint too.")

	text := message.String()

	fyne.Do(func() {
		if showToken && token != "" {
			g.tokenEntry.SetText(token)
			g.app.Clipboard().SetContent(token)
		}

		if fingerprint != "" {
			g.fingerprintEntry.SetText(fingerprint)
		}

		dialog.ShowInformation("Pairing", text, g.win)
	})
}

// stop closes the live session and stops the host, if any.
func (g *gui) stop() {
	g.mu.Lock()
	sess, server := g.sess, g.server
	g.sess, g.server = nil, nil
	g.mu.Unlock()

	g.updateButtons()
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

	g.entry.SetText("")
	g.addMessage(chatMessage{text: text})

	go func() {
		if err := sess.Send([]byte(text)); err != nil {
			g.setStatus("Send failed: " + err.Error())
		}
	}()
}

// scan walks the local subnet for a chat host on the configured port.
func (g *gui) scan() {
	data := g.data()

	g.scanBtn.Disable()
	g.setStatus(fmt.Sprintf("Scanning port %d…", data.Port))

	go func() {
		found := websocketcore.Scan(data)

		fyne.Do(func() {
			g.scanBtn.Enable()

			if len(found) == 0 {
				g.setStatus("No host answered on this network")

				return
			}

			for _, addr := range found {
				g.addBeacon(discovery.Beacon{Name: addr, Addr: addr, Port: data.Port})
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
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	fyne.Do(g.updateButtons)

	sess.OnMessage(func(m session.Message) {
		from := m.From
		if from == "" {
			// An unnamed peer is still a peer: without this it would render as
			// "You", which is the one sender it is not.
			from = "peer"
		}

		g.addMessage(chatMessage{from: from, text: string(m.Payload)})
	})

	sess.OnClose(func(reason string) {
		g.mu.Lock()
		if g.sess == sess {
			g.sess = nil
		}
		g.mu.Unlock()

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
	} else {
		g.sendBtn.Disable()
	}

	if hasSession || hasServer {
		g.stopBtn.Enable()
	} else {
		g.stopBtn.Disable()
	}
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
func (g *gui) updateChatRow(id widget.ListItemID, obj fyne.CanvasObject) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	row, ok := obj.(*widget.Label)
	if !ok {
		return
	}

	row.SetText(g.messages[id].line())

	width := row.Size().Width
	if g.chatList == nil || width <= 0 {
		return
	}

	row.Resize(fyne.NewSize(width, row.MinSize().Height))
	g.chatList.SetItemHeight(id, row.MinSize().Height)
}

// selectChatRow copies the message that was tapped.
//
// This is the copy gesture a chat app teaches: tap the line and the line is on
// the clipboard, instead of asking a finger to drag a selection handle through
// a tall, wrapping label.
func (g *gui) selectChatRow(id widget.ListItemID) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	g.copyToClipboard(g.messages[id].line())
	g.chatList.Unselect(id)
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

func (g *gui) showError(err error) {
	fyne.Do(func() { dialog.ShowError(err, g.win) })
}

// addBeacon adds a device once. A beacon heard twice is one device.
func (g *gui) addBeacon(beacon discovery.Beacon) {
	for _, existing := range g.beacons {
		if existing.Addr == beacon.Addr && existing.Port == beacon.Port {
			return
		}
	}

	g.beacons = append(g.beacons, beacon)
	g.beaconList.Refresh()
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
	data.AllowAnonymous = g.anonymousCheck.Checked

	return data
}

// tls returns the TLS settings. The certificate paths come from configDir, so
// the host writes where the app can actually write — on Android there is no
// $HOME to fall back on.
func (g *gui) tls() auth.TLS {
	dir := g.configDir()

	return auth.TLS{
		Enabled:     g.tlsCheck.Checked,
		Insecure:    g.insecureCheck.Checked,
		Fingerprint: strings.TrimSpace(g.fingerprintEntry.Text),
		CertFile:    filepath.Join(dir, "cert.pem"),
		KeyFile:     filepath.Join(dir, "key.pem"),
	}
}

func (g *gui) tokenFile() string {
	return filepath.Join(g.configDir(), "token")
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
