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
	"image"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
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
	updatepkg "github.com/fiwon123/cthrone/internal/update"
	"github.com/fiwon123/cthrone/internal/wire"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// openBluetoothSession and openBluetoothServer are the radio's two entry points.
// They are variables so a test can drive Connect and Host without an adapter:
// there is no radio in a test, and what is being checked is the window's side of
// the call — which target it names, what it shows, and how it stops.
//
// The two network dials are variables for the same reason: a test drives a
// dropped client and its reconnection without a host to talk to (#326).
var (
	openBluetoothSession = bluetoothcore.OpenSession
	openBluetoothServer  = bluetoothcore.OpenServer
	openWebsocketSession = websocketcore.Dial
	openNATSSession      = natscore.OpenSession
)

// copyHintDuration is how long the transcript's "Copied" confirmation stays on
// screen. A tap that shows nothing reads as a tap that did nothing.
const copyHintDuration = 2 * time.Second

// copyHintMargin keeps the "Copied" popup off the bottom edge of the window.
const copyHintMargin = 48

// reconnectBase is how long a dropped client waits before asking for its host
// again. Each attempt doubles the wait up to reconnectMax, so a host that comes
// back is met within a minute and one that does not is asked once a minute
// rather than all the time (#326).
//
// It is a variable so the tests can run the schedule in milliseconds: a test
// that waited a real second per attempt would be slow, not more convincing.
var reconnectBase = time.Second

// reconnectMax caps the wait. A client that has been away a while settles
// here instead of backing off to hours.
const reconnectMax = time.Minute

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
	hint        *widgets.Text

	// thisDevice is the read-only line that shows a hosting device its own
	// address. It is hidden until this device hosts: the Devices list never
	// lists this device, so this is where its address lives.
	thisDevice *widgets.Text

	// credential is the read-only line that names the credential in force and
	// where it came from — this device's key, or a shared secret typed here or
	// set in the environment. It also reports a token file an older version
	// left behind: that file decides nothing now, and a user who is told so is
	// the one who can stop looking for the cause in the wrong place.
	credential *widgets.Text

	// forgetSecret deletes the token file an older version read. It decides
	// nothing now, so the button is an offer to remove a leftover that used to
	// decide the model, not a switch.
	forgetSecret *widgets.CopyButton

	// about is the read-only tab: which build this is, where it comes from,
	// and — on a press, never on a start — whether there is a newer release.
	// aboutCredential repeats the Setup tab's line, because "which model am I
	// in?" belongs on the screen a bug report is written from.
	aboutCredential *widgets.Text
	aboutNote       *widgets.Text
	updateBtn       *widgets.CopyButton
	releaseBtn      *widgets.CopyButton
	installBtn      *widgets.CopyButton
	previousBtn     *widgets.CopyButton
	restartBtn      *widgets.CopyButton
	revertBtn       *widgets.CopyButton
	wikiBtn         *widgets.CopyButton

	// aboutOffer is the release the check found and the Install button offers;
	// empty means this build is the newest one, and the button is hidden.
	aboutOffer string

	// chat
	tabs      *widgets.Tabs
	status    *widgets.Text
	chatList  *widget.List
	entry     *widget.Entry
	sendBtn   *widgets.CopyButton
	stopBtn   *widgets.CopyButton
	attachBtn *widgets.CopyButton
	hideBtn   *widgets.CopyButton
	messages  []chatMessage

	// hidden is the composer's secret mode: while it is on the field is a
	// password entry, and the message carries the hidden flag (#469).
	hidden bool

	// sel is the range-copy state: while it is active the chat header swaps to
	// the selection controls, and a transcript tap marks an end instead of
	// copying a row (#469). headerHolder holds both headers so switching is one
	// visibility change rather than a rebuild.
	sel          selection
	headerHolder *fyne.Container
	selectHeader *fyne.Container
	selectBtn    *widgets.CopyButton
	copyAllBtn   *widgets.CopyButton
	clearAllBtn  *widgets.CopyButton
	selCount     *widget.Label
	selCopyBtn   *widgets.CopyButton
	selDeleteBtn *widgets.CopyButton
	selCancelBtn *widgets.CopyButton

	// lastPick is the folder the last attached file came from: the picker opens
	// there next time instead of walking the toolkit's place list (#386).
	lastPick string

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

	// audio is the window's one sound device, made on the first Play: a window
	// that never plays anything never touches the sound card (#306). audioPath
	// is the clip it is sounding, audioGen tells a decode or a clock ticker that
	// a newer action has superseded it, and audioClock is the row widget that
	// ticker writes to — dropped by whichever row is painted next, because the
	// list reuses that widget for another message.
	//
	// audioDurations remembers a length, audioPeaks the waveform, and
	// audioResumeAt where a stopped clip left its playhead; the decode runs
	// beside the UI, so the first two are guarded by mu, and the third is
	// written by Stop on the UI goroutine and read by a repaint. playableFiles
	// is the same answer per path, and is only touched while painting, like the
	// picture and text caches.
	audio          audioEngine
	audioPath      string
	audioGen       int
	audioDuration  time.Duration
	audioClock     *widgets.Text
	audioDurations map[string]time.Duration
	audioPeaks     map[string][]float32

	// audioPrepared remembers the clips whose length and waveform are already
	// known, and audioPreparing bounds the background decode to one at a time.
	// audioClip is the last decoded clip, kept so the Play that follows reading
	// its waveform does not decode the file a second time (#435).
	audioPrepared  map[string]bool
	audioPreparing bool
	audioClip      *audioClip
	audioClipPath  string

	// audioResumeAt is where the sound stopped in each clip, as a fraction, so
	// Play continues there instead of starting over (#409). A clip that played
	// to its end has no entry.
	audioResumeAt map[string]float64

	// perf is the message path's cost, counted while CTHRONE_PERF is set:
	// a report of "it lags" needs numbers from the device that lags, and this
	// is the cheapest way to get them (#432).
	perfEnabled bool
	perfCount   int
	perfTotal   time.Duration
	perfLast    time.Duration

	// stateWord is the short word beside the icon, and stateGlyph their colour
	// and shape. lost is whether the last session went away without the user
	// asking: it is what tells a drop (🔴) from a stop (⚪) (#453, #464).
	stateWord *widgets.Text
	// stateGlyph is the header's one-icon connection state, and lost is whether
	// the last session went away without the user asking: it is what tells a
	// drop (🔴) from a stop (⚪) (#453).
	stateGlyph *canvas.Text
	lost       bool

	// lastBottom is the transcript's scroll offset when the view was last at
	// the end, and jumpBtn is the way back down when the reader is not there.
	// A new message follows only if the reader was at the bottom (#434).
	lastBottom float32

	// scrollStop ends the scroll watcher when the window closes.
	scrollStop chan struct{}
	jumpBtn    *widgets.CopyButton
	jumpRow    *fyne.Container

	// video is the window's player for animated pictures (#421). Only one clip
	// is kept: an animation is large, and a transcript that scrolled through ten
	// would hold all ten. videoSurface and videoClock are the widgets of the row
	// currently showing that clip, dropped by whichever row is painted next the
	// way the audio clock's are.
	video         *videoPlayer
	videoPath     string
	videoClip     *videoClip
	videoClipPath string
	videoFrame    image.Image
	videoSurface  *canvas.Image
	videoClock    *widgets.Text
	videoSyncing  bool
	videoFiles    map[string]bool
	playableFiles map[string]bool

	// devices
	beacons    []device
	beaconList *widget.List
	scanBtn    *widgets.CopyButton
	listHint   *widgets.Text
	listener   *discovery.Listener

	mu   sync.Mutex
	sess session.Session

	// hub is the room this window hosts, while it hosts. A host's own message
	// goes through it, not through the one session `sess` happens to hold: a
	// host with two devices used to reach only the last one that joined (#320).
	hub    *session.Hub
	server liveServer

	// openServer is the one-file loopback server behind a phone's Open (#470).
	// It is closed by the next Open, after openServeWindow, or at shutdown.
	openServer *http.Server

	// liveSessions is how many sessions this window holds. A host holds one per
	// device in its room, and the receiver is prepared for the first and closed
	// with the last (#342).
	liveSessions int

	// What the transcript has already decoded or read, by path, so a repaint
	// does not do the I/O again (#321). Painting is on the UI goroutine, but a
	// background prepare refreshes the transcript when it finishes, and the
	// test driver runs that inline on the prepare's goroutine — so the caches
	// take cacheMu rather than trusting the thread (#435).
	cacheMu        sync.Mutex
	pictures       map[string]fyne.Resource
	pictureOrder   []string
	picturePending map[string]bool
	decodableFiles map[string]bool
	texts          map[string]string

	// peerName is who a session is with when the handshake named one. The state
	// itself is written on the chat header's one line, in g.status (#345, #383).
	peerName string

	// updateTag and updateAt are the last successful check and when it ran, so a
	// second press is answered from memory (#322).
	updateTag string
	updateAt  time.Time

	// composerRows is the row count the message field is showing, so a keystroke
	// that does not change it does not re-lay the field out (#324).
	composerRows int

	// composerActions is the row of attach/hide/send controls. It is kept so a
	// test can prove a growing message grows the field and not the buttons
	// (#474).
	composerActions *fyne.Container

	// lastComposer is the text before the current change, so an appended newline
	// can be told from a typed one, and editingComposer marks the changes this
	// window makes itself (#323).
	lastComposer    string
	editingComposer bool

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

	// link is the client connection this window dialled, kept so a session
	// that ends on its own can be asked for again with the same host,
	// transport and credential (#326). It is nil while this window hosts and
	// while it has never connected: a host is what others reconnect to, not
	// something that reconnects.
	link *dialLink

	// reconnecting says a dropped client is being retried, which the header
	// shows. retryCtx is the generation that owns the retry, so a Stop, a new
	// Host/Connect or a second drop makes an older attempt a no-op instead of
	// a second loop.
	reconnecting bool
	retryCtx     context.Context
	retryCancel  context.CancelFunc
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

	g := &gui{app: a, win: w, mobile: mobile, video: &videoPlayer{}, perfEnabled: os.Getenv("CTHRONE_PERF") != ""}

	// A window that cannot be built has nothing to show, and a blank window is
	// a worse answer than a report: the panic propagates to the recover above.
	g.build()

	// The way down follows the reader, not only the messages: Fyne's List keeps
	// its own scroll callback, so the position is sampled (#508).
	g.scrollStop = make(chan struct{})
	go g.watchScroll()

	// Closing the window is a stop. The desktop process exits anyway, but the
	// phone can outlive its window: a listener left bound there makes the next
	// Host fail with "address already in use".
	w.SetCloseIntercept(func() {
		g.shutdown()
		w.Close()
	})

	go g.pruneLoop()

	// Everything that touches the disk or binds a socket runs beside the window,
	// not in front of it: the identity read and the discovery listener used to
	// happen before ShowAndRun, so the user waited for both before anything was
	// painted. Nothing here blocks the main goroutine, which goes straight to
	// the event loop and draws.
	go func() {
		if err := guard("identity", func() {
			if err := g.fillIdentity(); err != nil {
				logFailure(err)
			}
		}); err != nil {
			logFailure(err)
		}

		if mobile {
			// Discovery is skipped on a phone: the beacon and the scan are for
			// finding another machine on a network, and the app that is already
			// open is the one the user picked up.
			return
		}

		// Discovery and the resize are not worth the window: a failure in
		// either is recorded and the chat still opens.
		if err := guard("discovery", g.startDiscovery); err != nil {
			logFailure(err)
		}

		if err := guard("resize", func() {
			fyne.Do(func() { w.Resize(fyne.NewSize(900, 640)) })
		}); err != nil {
			logFailure(err)
		}
	}()

	w.ShowAndRun()

	// The close intercept may not run on every platform — a phone that kills
	// the window rather than closing it — so the same release happens here.
	g.shutdown()

	return nil
}

// loadIdentity is a variable so a test can answer without the real
// configuration directory, and can see whether the build asked at all.
var loadIdentity = discovery.LoadIdentity

// fillIdentity names this device after the identity on disk.
//
// It runs after the window is shown, off the UI goroutine: the read is I/O, and
// a window that waits for the disk to appear is the delay this function exists
// to avoid.
func (g *gui) fillIdentity() error {
	identity, err := loadIdentity()
	if err != nil {
		return err
	}

	if identity == nil || identity.Name == "" {
		return nil
	}

	fyne.Do(func() {
		// A name the user has already typed wins: this is a default, not an
		// instruction.
		if g.nameEntry == nil || g.nameEntry.Text != "" {
			return
		}

		g.nameEntry.SetText(identity.Name)
	})

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

	// The window's player exists before any row asks for it: a test builds a
	// gui without Run's struct literal.
	if g.video == nil {
		g.video = &videoPlayer{}
	}

	// --- chat ---------------------------------------------------------
	// The chat header is one row: the status, and the two actions that belong
	// to the conversation rather than to a message — Stop and Files. It used to
	// be three: a state line, a copy hint, and the status under them, which on a
	// phone wrapped and said the same thing twice (#383).
	//
	// The status line is where the window stands when it has nothing newer to
	// say — offline, hosting, connected to whom — so the state is still there a
	// second after the window opens, and a real message ("Cannot play …") wins
	// over it until it is cleared. It never carries the app's name: on a phone
	// that was the whole left side, spent on the one word the user already knows
	// (#345).
	//
	// There is no Copy all here any more: it copied the transcript as one
	// string, which is not what a user who wants one message means, and a tap on
	// a row already copies that row (#289).
	// The state is one icon, always there, and the line is for what happened:
	// the words that repeated the state were the report (#433), and losing the
	// state altogether was the next one (#453).
	g.stateGlyph = canvas.NewText("⚪", theme.Color(theme.ColorNameDisabled))
	g.stateGlyph.TextSize = 18
	g.stateGlyph.TextStyle = fyne.TextStyle{Bold: true}

	// One word beside it: the icon alone is not enough to read, and the address
	// a host prints belongs on its own line, not here (#464).
	g.stateWord = g.text("offline")
	g.stateWord.SetTextStyle(fyne.TextStyle{Bold: true})

	g.status = g.text("")
	g.status.SetTextStyle(fyne.TextStyle{Bold: true})
	g.status.SetOnTapped(func() {
		if text := g.status.Text(); text != "" {
			g.copyToClipboard(text)
		}
	})
	g.status.Hide()

	// The Files action lives beside the transcript rather than in Setup: it is
	// about what happens to a received file, which is a chat concern, and Setup
	// is the connection form.
	filesLabel := "Files"
	if g.lay.Stack {
		filesLabel = ""
	}

	filesBtn := widgets.NewButtonWithIcon("Files", theme.FolderIcon(), g.showFiles, g.copyLabel)
	filesBtn.SetText(filesLabel)

	// Stop is a connection control, not a message action: it belongs with the
	// session's own switches on Setup, where a thumb aiming at Send or at a row
	// cannot reach it by mistake (#288, #508). The header still says whether a
	// session is running, so stopping is not the only signal.
	stopLabel := "Stop"
	if g.lay.Stack {
		stopLabel = ""
	}

	g.stopBtn = widgets.NewButtonWithIcon("Stop", theme.MediaStopIcon(), g.stop, g.copyLabel)
	g.stopBtn.SetText(stopLabel)
	g.stopBtn.Disable()

	// The way back to the end of the transcript: it sits in a row of its own
	// just above the composer, where the eye already is, and it is shown whenever
	// the reader is somewhere up the history — not only when a message arrives
	// (#434, #508).
	g.jumpBtn = widgets.NewButtonWithIcon("Go to last message", theme.MoveDownIcon(), g.scrollToLatest, g.copyLabel)
	if g.lay.Stack {
		g.jumpBtn.SetText("")
	}
	g.jumpBtn.Hide()

	// Copy all is the transcript action the older header dropped (#289): a
	// reader who wants the whole conversation back with the button that says
	// so, and not a row at a time (#469).
	g.copyAllBtn = widgets.NewButtonWithIcon("Copy all", theme.ContentCopyIcon(), g.copyAll, g.copyLabel)
	g.copyAllBtn.Disable()

	// Clear all is its own action, not a corner of the selection header: emptying
	// the transcript is one press and one question, and selecting specific
	// messages is a different job (#508).
	g.clearAllBtn = widgets.NewButtonWithIcon("Clear all", theme.ContentClearIcon(), g.clearTranscript, g.copyLabel)
	g.clearAllBtn.Disable()

	// Select is the mobile-friendly copy gesture: two taps pick the ends of a
	// span, and the header becomes the actions that span has (#469).
	g.selectBtn = widgets.NewButtonWithIcon("Select", theme.ListIcon(), g.beginSelection, g.copyLabel)
	g.selectBtn.Disable()

	if g.lay.Stack {
		g.copyAllBtn.SetText("")
		g.clearAllBtn.SetText("")
		g.selectBtn.SetText("")
	}

	header := container.NewHBox(g.stateGlyph, g.stateWord)
	header.Add(layout.NewSpacer())
	header.Add(g.copyAllBtn)
	header.Add(g.clearAllBtn)
	header.Add(g.selectBtn)
	header.Add(filesBtn)

	// The selection header replaces the state row while a range is being built:
	// the state is not what the reader is doing then, and five actions do not
	// fit beside a word that says nothing about them (#469).
	g.selCount = widget.NewLabel("0 selected")

	g.selCopyBtn = widgets.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), g.copySelection, g.copyLabel)
	g.selDeleteBtn = widgets.NewButtonWithIcon("Delete", theme.DeleteIcon(), g.deleteSelection, g.copyLabel)
	g.selCancelBtn = widgets.NewButtonWithIcon("Cancel", theme.CancelIcon(), g.endSelection, g.copyLabel)

	if g.lay.Stack {
		for _, button := range []*widgets.CopyButton{g.selCopyBtn, g.selDeleteBtn, g.selCancelBtn} {
			button.SetText("")
		}
	}

	g.selectHeader = container.NewHBox(
		g.selCount,
		layout.NewSpacer(),
		g.selCopyBtn,
		g.selDeleteBtn,
		g.selCancelBtn,
	)
	g.selectHeader.Hide()
	g.headerHolder = container.NewStack(header, g.selectHeader)

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
			row := g.text("")

			pic := canvas.NewImageFromResource(nil)
			pic.FillMode = canvas.ImageFillContain
			pic.Hide()

			// A text file is drawn, not just named: a pasted log is unreadable
			// as a filename, and the beginning of it is what the sender meant
			// the other person to see.
			preview := g.text("")
			preview.Hide()

			save := widgets.NewButtonWithIcon("Save as…", theme.DocumentSaveIcon(), nil, g.copyLabel)
			save.Hide()

			// A picture can go to the system clipboard as a picture: Fyne has no
			// image clipboard, so the platform's own tool does it (#510).
			copyPic := widgets.NewButtonWithIcon("Copy", theme.ContentCopyIcon(), nil, g.copyLabel)
			copyPic.Hide()

			// A file neither drawn nor read here — a PDF, an audio clip — is
			// opened by whatever the device uses for that kind of file. It is
			// the one thing this program can offer without an audio or PDF
			// engine of its own.
			open := widgets.NewButtonWithIcon("Open", theme.DocumentIcon(), nil, g.copyLabel)
			open.Hide()

			// The icon says what kind of file a row is, for the rows that draw
			// nothing of their own: a PDF and a song were indistinguishable
			// (#304).
			icon := widget.NewIcon(theme.FileIcon())
			icon.Hide()

			// A clip this build can decode plays in the row itself: seven
			// seconds of speech does not deserve a second window, and on a
			// phone handing the file to another app was the only option left
			// (#305, #306). The clock beside the button is the length, or how
			// far into the clip the sound has got.
			audio := newAudioRow(g)

			// An animated picture plays on its own surface, with a seek bar: the
			// controller #421 added is what FFmpeg would later feed too.
			video := newVideoRow(g)

			// The sender has a line of its own, in a colour of its own, so a room
			// of three is readable at a glance (#346). It is a selectable text
			// rather than a canvas.Text: the name is text a reader may want to
			// copy, and a canvas.Text cannot be selected at all (#503). The
			// colour comes from the same name hash, through the label's
			// importance, so the palette is the one it had.
			sender := g.text("")
			sender.SetTextStyle(fyne.TextStyle{Bold: true})
			sender.AllowSelection()

			// A hidden message carries its eye: press it to reveal, and again to
			// hide. A message that was never hidden has no control, because there
			// is nothing to uncover (#496, #503). It shares the sender line with
			// the copy button so the two are the same square — in the caption's
			// border the eye was stretched to the row's height.
			reveal := widgets.NewButtonWithIcon("", theme.VisibilityOffIcon(), nil, g.copyLabel)
			reveal.SetCopyText("")
			reveal.Hide()

			// The copy action sits at the end of the sender's line, so every
			// message can be copied without spending a line of its own and
			// without the tap that used to do it (#496).
			copyMessage := widgets.NewButtonWithIcon("", theme.ContentCopyIcon(), nil, g.copyLabel)
			copyMessage.SetCopyText("Copy the message")

			caption := container.NewVBox(container.NewHBox(sender, layout.NewSpacer(), reveal, copyMessage), row)

			return container.NewVBox(container.NewBorder(nil, nil, icon, nil, caption), pic, preview, save, open, audio.box, video.box, copyPic)
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
	withClear(g.entry)
	g.entry.SetMinRowsVisible(1)
	// No placeholder: the field says nothing until the user types, and the
	// keyboard's behaviour belongs in the docs, not in the message (#344).
	g.entry.OnChanged = g.composerChanged
	g.entry.OnSubmitted = func(text string) {
		// On a phone the keyboard's action key is the send key, and Fyne routes
		// Return through OnSubmitted only when its hidden shift flag is set —
		// which nothing on a phone controls. So the phone's rule is stated here
		// rather than inferred from the text (#362): an action key sends, a typed
		// newline breaks the line, and the Send button always sends.
		if g.mobile {
			g.send(text)

			return
		}

		g.insertComposerNewline()
	}

	g.sendBtn = widgets.NewButtonWithIcon("Send", theme.MailSendIcon(), func() { g.send(g.entry.Text) }, g.copyLabel)
	// Send is the primary action of the composer: the filled (high) importance
	// makes it the thing a thumb finds without reading (#324).
	g.sendBtn.SetImportance(widget.HighImportance)
	g.sendBtn.Disable()

	g.attachBtn = widgets.NewButtonWithIcon("Attach", theme.MailAttachmentIcon(), g.pickFile, g.copyLabel)
	// The paperclip is the label: in a chat composer the icon is the word, and
	// the row's width belongs to the field (#324).
	g.attachBtn.SetText("")
	g.attachBtn.Disable()

	// The composer's eye is the password switch: an eye that masks the field
	// while a password or a key is typed, and shows it again on the next press
	// (#469). The row you send carries the flag, so the other screen masks it
	// too (#474).
	g.hideBtn = widgets.NewButtonWithIcon("Hide", theme.VisibilityOffIcon(), g.toggleHidden, g.copyLabel)
	g.hideBtn.SetText("")
	g.hideBtn.SetCopyText("Send as password")

	// The queue starts with the window and outlives any one session: a file
	// picked while the connection is being replaced still has a worker to hand
	// it to, and that worker reports the failure if there is nothing to send to.
	g.outbox = make(chan outgoing, 32)
	go g.pumpOutbox()

	var input fyne.CanvasObject
	if g.lay.Stack {
		// Touch: the field on its own line, then the actions. Send takes the
		// rest of the row beside the two icons, so a thumb does not aim at a
		// blank half-cell, and the row keeps its height while the field grows
		// (#474).
		g.composerActions = container.NewBorder(nil, nil, container.NewHBox(g.attachBtn, g.hideBtn), nil, g.sendBtn)
		input = container.NewVBox(g.entry, g.composerActions)
	} else {
		g.composerActions = container.NewHBox(g.attachBtn, g.hideBtn, g.sendBtn)
		input = container.NewBorder(nil, nil, nil, g.composerActions, g.entry)
	}

	// The status sits under the transcript, above the composer: it is a bar for
	// what just happened, not a second header line. The header answers "where
	// do I stand" with the state icon, and nothing else (#457).
	// The way back down is a row of its own above the composer: centred, always
	// in the same place, and away from the header's actions (#508).
	// The way down keeps its height when the button is hidden: a row that comes
	// and goes resizes the transcript under the reader, which moved the last line
	// out of reach as the button appeared (#517).
	jumpSize := fyne.NewSize(240, 40)
	if g.lay.Stack {
		jumpSize = fyne.NewSize(56, 40)
	}

	g.jumpRow = container.NewHBox(
		layout.NewSpacer(),
		container.NewGridWrap(jumpSize, g.jumpBtn),
		layout.NewSpacer(),
	)

	chat := container.NewBorder(
		container.NewVBox(container.NewPadded(g.headerHolder), widget.NewSeparator()),
		container.NewVBox(container.NewPadded(g.status), g.jumpRow, container.NewPadded(input)),
		nil, nil,
		g.chatList,
	)

	// --- setup --------------------------------------------------------
	g.portEntry = widget.NewEntry()
	g.portEntry.SetText(strconv.Itoa(defaultPort))

	g.bindEntry = widget.NewEntry()
	withClear(g.bindEntry)
	g.bindEntry.SetPlaceHolder("every interface")

	// The name is filled in after the window is up (fillIdentity): reading it
	// is I/O, and the window must not wait on the disk to appear.
	g.nameEntry = widget.NewEntry()
	withClear(g.nameEntry)
	g.nameEntry.SetPlaceHolder("device name")

	g.hostEntry = widget.NewEntry()
	withClear(g.hostEntry)
	g.hostEntry.SetPlaceHolder("192.168.1.100")

	g.tokenEntry = widget.NewPasswordEntry()
	g.tokenEntry.SetPlaceHolder("empty: pair by key, with nothing to copy")
	g.tokenEntry.OnChanged = func(string) { g.refreshCredential() }

	g.fingerprintEntry = widget.NewEntry()
	withClear(g.fingerprintEntry)
	g.fingerprintEntry.SetPlaceHolder("certificate fingerprint (optional: pinned on first contact)")

	g.subjectEntry = widget.NewEntry()
	withClear(g.subjectEntry)
	g.subjectEntry.SetText(natscore.DefaultSubject)

	g.natsURLEntry = widget.NewEntry()
	withClear(g.natsURLEntry)
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

	g.hint = g.text("")

	g.thisDevice = g.text("")
	g.thisDevice.Hide()

	g.credential = g.text("")

	g.forgetSecret = widgets.NewButtonWithIcon("Delete the old token file", theme.ContentClearIcon(), g.forgetSecretFile, g.copyLabel)
	g.forgetSecret.Hide()

	setup := container.NewVScroll(container.NewPadded(container.NewVBox(
		g.heading("Quick Setup"),
		g.formHolder,
		g.credential,
		g.forgetSecret,
		widget.NewSeparator(),
		actions,
		// Stop lives here rather than in the chat header: ending a session is
		// the same kind of decision as starting one (#508).
		g.stopBtn,
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
		// A row does something when pressed, so it looks like what it is: a
		// button (#287). The transcript's long-press text is not used here any
		// more — a sentence that is selectable reads as text, not as an action,
		// which is what the report said. The address stays copyable where it is
		// used: the Setup field it fills and the status line naming it.
		func() fyne.CanvasObject {
			row := widget.NewButton("", nil)
			row.Alignment = widget.ButtonAlignLeading

			return row
		},
		g.updateBeaconRow,
	)
	g.beaconList.OnSelected = g.selectBeacon

	// The beacon hint wraps on a phone, where the button and the sentence do
	// not share a line without the sentence running off the screen.
	var beaconHeader fyne.CanvasObject
	g.listHint = g.text(scanHint(transportWebsocket))
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

	// The security switches sit where they act: after the transport they belong
	// to, before the options that change what it dials. They are rows of the
	// form rather than lines above it, so a user who changes the device name and
	// the address — which is what nearly every run does — reaches them first
	// (#291).
	rows = append(rows,
		widgets.Row{Caption: "", Field: g.tlsCheck},
		widgets.Row{Caption: "", Field: g.insecureCheck},
	)

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
//
// A retry a dropped client left running is cancelled here: the user is asking
// for a different connection, and two loops dialling would race to become the
// live one (#326).
func (g *gui) beginStart() (int, bool) {
	g.mu.Lock()

	if g.starting || g.stopping || g.sess != nil || g.server != nil {
		g.mu.Unlock()
		return 0, false
	}

	cancel := g.retryCancel
	g.reconnecting = false
	g.retryCtx, g.retryCancel = nil, nil

	g.startSeq++
	g.starting = true
	seq := g.startSeq

	g.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	fyne.Do(g.refreshStateGlyph)

	return seq, true
}

// finishStart ends attempt seq and, when it is still the current one, runs
// install under the same lock — so the state it publishes cannot be seen
// half-made, and a press that arrives in between is refused rather than
// starting a second transport. false means Stop arrived first: whatever the
// attempt opened must be closed instead of installed.
func (g *gui) finishStart(seq int, install func()) bool {
	g.mu.Lock()

	g.starting = false

	if g.startSeq != seq {
		g.mu.Unlock()

		return false
	}

	if install != nil {
		install()
	}

	g.mu.Unlock()

	// The attempt is over and whatever it opened is installed: the icon now
	// says hosting rather than starting, without waiting for a device to join
	// (#453). It is outside the lock because fyne.Do can run the redraw inline.
	fyne.Do(g.refreshStateGlyph)

	return true
}

// dialLink is everything a Connect asks of a transport, captured when the user
// starts it. A reconnection dials with the choices the session began with, not
// with whatever the form holds by the time it tries again.
type dialLink struct {
	mode    transportMode
	host    string
	data    *appdata.Data
	tls     auth.TLS
	opts    pairing.Options
	broker  string
	subject string
}

// dialClient opens the client session a link describes. Connect and a
// reconnection call the same function, so the two cannot drift apart.
func dialClient(l *dialLink) (session.Session, error) {
	switch l.mode {
	case transportNATS:
		return openNATSSession([]string{l.subject}, l.data, l.opts, l.broker, l.tls)

	case transportBluetooth:
		return openBluetoothSession(targetArg(l.host), l.data, l.opts)

	default:
		return openWebsocketSession([]string{l.host}, l.data, l.opts, l.tls)
	}
}

// connectedStatus is what the window says when a link is up, in the words of
// the transport it is on. The first connection and a restored one differ only
// in the verb, so a user can tell that the window came back by itself.
func connectedStatus(l *dialLink, again bool) string {
	verb := "Connected"
	if again {
		verb = "Reconnected"
	}

	switch l.mode {
	case transportNATS:
		return verb + " through NATS on " + l.broker

	case transportBluetooth:
		return verb + " over Bluetooth — chatting"

	default:
		return verb + " to " + l.host + " — chatting"
	}
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

	// The form is read once, here: a reconnection replays this link rather
	// than whatever the entries hold a minute later (#326).
	link := &dialLink{
		mode:    mode,
		host:    host,
		data:    g.data(),
		tls:     g.tls(),
		opts:    g.pairingOptions(),
		broker:  g.natsURLEntry.Text,
		subject: g.subject(),
	}

	g.setStatus("Connecting…")
	g.tabs.SelectIndex(0)
	g.updateButtons()

	go func() {
		sess, err := dialClient(link)

		if !g.finishStart(seq, func() {
			// Only a dial that worked becomes the window's link: a failed
			// attempt leaves nothing to reconnect to (#326).
			if err == nil {
				g.sess = sess
				g.link = link
			}
		}) {
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

		g.setStatus(connectedStatus(link, false))
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

	if mode != transportNATS {
		route, _ := defaultRouteIP()
		addrs := discovery.AdvertisedAddrs(localAddrs(), route)
		own = thisDeviceText(addrs, data.Port)
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

			g.attach(sess)
			g.tabs.SelectIndex(0)

			return

		case transportBluetooth:
			g.hostBluetooth(seq, data, opts)
		default:
			g.hostWebsocket(seq, data, tlsConf, opts, own, typedToken)
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

	if !g.finishStart(seq, func() {
		g.server = handle
		g.hub = hubOf(handle)
	}) {
		_ = handle.Stop()
		fyne.Do(g.updateButtons)

		return
	}

	fyne.Do(g.updateButtons)
	g.tabs.SelectIndex(0)
	g.offerPairing("", handle.DeviceFingerprint, "", "", false)
}

// hostWebsocket serves the websocket room: it binds the port, shows this
// device's address, and prints what the other device needs to find it.
func (g *gui) hostWebsocket(seq int, data *appdata.Data, tlsConf auth.TLS, opts pairing.Options, own string, typedToken bool) {
	handle, err := websocketcore.OpenServer(data, opts, tlsConf, func(sess session.Session) {
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
	if !g.finishStart(seq, func() {
		g.server = handle
		g.hub = hubOf(handle)
	}) {
		_ = handle.Stop()
		fyne.Do(g.updateButtons)

		return
	}

	fyne.Do(g.updateButtons)
	g.showThisDevice(own)
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
	g.stopAudio()

	if g.chatList != nil {
		g.chatList.Refresh()
	}

	g.mu.Lock()
	sess, server := g.sess, g.server
	cancel := g.retryCancel
	g.sess, g.server, g.hub = nil, nil, nil
	g.link = nil
	g.reconnecting = false
	g.retryCtx, g.retryCancel = nil, nil
	g.liveSessions = 0
	g.lost = false

	// A Start that is still in flight is cancelled: its server closes itself
	// when it opens instead of becoming the live one. Until this stop has
	// released the listener, neither Host nor Connect may run.
	g.startSeq++
	g.stopping = true
	g.mu.Unlock()

	if cancel != nil {
		cancel()
	}

	g.finishReceive()
	g.hideThisDevice()
	g.setStatus("Stopping…")
	g.updateButtons()
	g.refreshStateGlyph()

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
	stop := g.scrollStop
	g.scrollStop = nil
	g.mu.Unlock()

	if stop != nil {
		close(stop)
	}

	// The device goes back to the machine before the process does, and no
	// widget is touched: there is no event loop left to return to.
	g.stopAudio()

	// A file being served to the browser stops with the window: a listener
	// left behind would keep a private file reachable after the app is gone.
	g.closeOpenServer()

	g.mu.Lock()
	sess, server := g.sess, g.server
	cancel := g.retryCancel
	g.sess, g.server, g.hub = nil, nil, nil
	g.link = nil
	g.reconnecting = false
	g.retryCtx, g.retryCancel = nil, nil
	g.liveSessions = 0
	g.startSeq++
	g.mu.Unlock()

	if cancel != nil {
		cancel()
	}

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

	sender, err := g.outgoing()
	if err != nil {
		g.setStatus("Nothing to send to — " + err.Error())

		return
	}

	// Text goes through the envelope, which is what keeps the one string the
	// receiver would misread — a message beginning with the magic — from
	// arriving as a damaged frame: Encode wraps that line in a text frame, and
	// the receiver unwraps it. A hidden message is wrapped for the same reason:
	// the flag has to travel in the header (#469). For every other message
	// Encode hands the bytes back unchanged, so a peer that predates the
	// envelope still reads it.
	hidden := g.hidden

	payload, err := wire.Encode(wire.Header{Kind: wire.KindText, Hidden: hidden}, []byte(text))
	if err != nil {
		g.setStatus("Not sent: " + err.Error())

		return
	}

	g.entry.SetText("")
	g.addMessage(chatMessage{text: text, hidden: hidden})
	g.scrollToLatest()

	go func() {
		if err := sender.Send(payload); err != nil {
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
				g.addMessage(chatMessage{from: "system", text: "No host answered on this network"})

				return
			}

			g.setStatus(fmt.Sprintf("Found %d host(s) — tap one, then Connect", len(found)))
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
				g.addMessage(chatMessage{from: "system", text: "No cthrone device answered over Bluetooth"})

				return
			}

			g.setStatus(fmt.Sprintf("Found %d Bluetooth device(s) — tap one, then Connect", found))
		})
	}()
}

// updateBeaconRow fills one device row: the text, and the tap the list would
// have handled before the row became a long-press widget.
//
// It is a method rather than a closure so a test can drive the real binding
// instead of rebuilding what it thinks the binding does.
func (g *gui) updateBeaconRow(id widget.ListItemID, obj fyne.CanvasObject) {
	rows := g.visibleBeacons()
	if id < 0 || id >= len(rows) {
		return
	}

	button, ok := obj.(*widget.Button)
	if !ok {
		return
	}

	button.SetText(deviceLabel(rows[id].Beacon))
	button.OnTapped = func() { g.beaconList.Select(id) }
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

	// The log line is for the report that says "I cannot click the device": it
	// answers the one question a screenshot cannot — did the tap arrive at all,
	// and was there a row for it — without asking the user to read code.
	if id < 0 || id >= len(rows) {
		logEvent(fmt.Sprintf("device row %d tapped with %d rows shown", id, len(rows)))

		return
	}

	beacon := rows[id]
	logEvent("device row tapped: " + deviceLabel(beacon.Beacon))

	g.hostEntry.SetText(beacon.Addr)

	// A radio address has no port: the row offers the device, not an address on
	// a network, and the port field is not on screen in that mode anyway.
	if beacon.Port > 0 {
		g.portEntry.SetText(strconv.Itoa(beacon.Port))
	}

	g.setStatus("Selected " + deviceLabel(beacon.Beacon) + " — press Connect")
	g.tabs.SelectIndex(1)
	g.beaconList.UnselectAll()
}

// --- session plumbing -------------------------------------------------

// attach makes sess the live session and renders what it carries.
// sessionJoined counts one more live session and reports whether it is the
// first, which is when the receiver has to be prepared.
func (g *gui) sessionJoined() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.liveSessions++

	return g.liveSessions == 1
}

// sessionLeft counts one out and reports whether the room is now empty, which is
// when the receiver goes away.
func (g *gui) sessionLeft() bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.liveSessions > 0 {
		g.liveSessions--
	}

	return g.liveSessions == 0
}

func (g *gui) attach(sess session.Session) {
	// The receiver belongs to the room, not to a session: a half-received file
	// belongs to the connection that was carrying it, and the next connection
	// starts clean — but a *device* joining a room is not a new connection for
	// the one already in it.
	//
	// Preparing on every join closed the part file a running transfer was
	// writing (#286); preparing only when g.sess was nil was worse in the other
	// direction, because g.sess is just the newest session — when *it* left, the
	// receiver went with it while the rest of the room was still there, and a
	// file from a remaining device was refused with "no session was receiving"
	// (#342). So the count of live sessions decides.
	if g.sessionJoined() {
		g.prepareReceive()
	}

	g.mu.Lock()
	g.sess = sess
	g.lost = false
	g.mu.Unlock()

	fyne.Do(g.updateButtons)
	fyne.Do(g.refreshStateGlyph)

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
			g.mu.Lock()
			g.peerName = p.Name()
			g.mu.Unlock()

			peer.set(p.Name())

			if p.First && p.Fingerprint != "" {
				// First contact is the one moment the fingerprint can be
				// compared, so it goes on screen and in the transcript; a
				// later contact only says who it is. Both values are shown,
				// because the other screen has them under the opposite labels.
				g.addMessage(chatMessage{
					from: "system",
					text: "paired with " + p.Name() + " — first contact; its fingerprint is " + p.Fingerprint +
						", this device's is " + sealed.Fingerprint() + ". Compare both with the other screen.",
				})

				return
			}

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

		if g.sessionLeft() {
			g.finishReceive()
		}

		if closed {
			// The session this window was using is gone without the user
			// asking, which is what the red icon is for; a retry that starts
			// will move it to amber (#453).
			g.mu.Lock()
			g.lost = true
			g.mu.Unlock()

			// A session the user ended is not a peer who left: Stop clears the
			// live session before it closes it, so only a close that arrives
			// while this session is still the live one leaves a line. That is
			// exactly the case a host has to be able to read afterwards.
			g.addMessage(chatMessage{from: "system", text: disconnectLine(peer.get(), reason)})
		}

		fyne.Do(g.refreshStateGlyph)

		fyne.Do(g.updateButtons)
		g.setStatus("Disconnected: " + reason)

		// A close that arrives while this session is still the live one is a
		// link that went away without the user asking: Stop and the window
		// closing clear the session before they close it. A client asks for
		// that link again; a host has nothing to dial, and a refusal is an
		// answer rather than a drop, so neither starts a loop (#326).
		if closed {
			g.reconnectAfterDrop(sess, reason)
		}
	})
}

// reconnectAfterDrop starts the retry loop when a session that just closed was
// a client the window should ask for again.
func (g *gui) reconnectAfterDrop(sess session.Session, reason string) {
	// A refusal is an answer, not a drop: dialling again would ask the same
	// question of a peer that has already said no (#326). The session records
	// it as an error identity, so this does not read the close reason.
	if refused, ok := sess.(interface{ Refusal() error }); ok && refused.Refusal() != nil {
		return
	}

	g.mu.Lock()
	link := g.link
	if link == nil || g.reconnecting || g.stopping {
		g.mu.Unlock()
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	g.reconnecting = true
	g.retryCtx = ctx
	g.retryCancel = cancel
	g.mu.Unlock()

	fyne.Do(g.updateButtons)
	fyne.Do(g.refreshStateGlyph)

	go g.retryLink(ctx, link, reason)
}

// retryLink asks for the same link again until it is back, the user stops, or
// a refusal answers the question.
func (g *gui) retryLink(ctx context.Context, link *dialLink, reason string) {
	for attempt := 1; ; attempt++ {
		wait := reconnectDelay(attempt)
		g.setStatus(fmt.Sprintf("Disconnected: %s — reconnecting in %s (attempt %d)", reason, reconnectWait(wait), attempt))

		timer := time.NewTimer(wait)

		select {
		case <-ctx.Done():
			timer.Stop()

			return
		case <-timer.C:
		}

		sess, err := dialClient(link)

		if err != nil {
			if refusedDial(err) {
				g.endReconnect(ctx, "Disconnected: "+err.Error())

				return
			}

			continue
		}

		if !g.takeReconnect(ctx, sess) {
			sess.Close("cancelled")

			return
		}

		g.noteFirstContact(sess)
		g.attach(sess)
		g.setStatus(connectedStatus(link, true))
		g.addMessage(chatMessage{from: "system", text: "reconnected"})
		fyne.Do(g.updateButtons)

		return
	}
}

// takeReconnect installs a session a retry dialled, unless a Stop, a new start,
// or a later attempt already took over. It sets g.sess under the same lock so a
// Connect pressed in between is refused rather than racing this one.
func (g *gui) takeReconnect(ctx context.Context, sess session.Session) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.reconnecting || g.retryCtx != ctx {
		return false
	}

	g.reconnecting = false
	g.retryCtx, g.retryCancel = nil, nil
	g.sess = sess

	return true
}

// endReconnect stops the loop this ctx owns and leaves the window offline with
// the sentence in status: how a refusal answers the question the loop was
// asking.
func (g *gui) endReconnect(ctx context.Context, status string) {
	g.mu.Lock()
	owns := g.retryCtx == ctx
	if owns {
		g.reconnecting = false
		g.retryCtx, g.retryCancel = nil, nil
	}
	g.mu.Unlock()

	if !owns {
		return
	}

	g.mu.Lock()
	g.lost = true
	g.mu.Unlock()

	g.setStatus(status)
	fyne.Do(g.updateButtons)
	fyne.Do(g.refreshStateGlyph)
}

// reconnectDelay is how long to wait before attempt n, counting from 1: the
// base, doubling, capped at reconnectMax.
func reconnectDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}

	delay := reconnectBase
	for i := 1; i < attempt && delay < reconnectMax; i++ {
		delay *= 2
	}

	if delay > reconnectMax {
		return reconnectMax
	}

	return delay
}

// reconnectWait is the delay as the status line says it: whole seconds, so
// "in 4s" rather than "in 4.0000001s".
func reconnectWait(d time.Duration) string {
	if d >= time.Minute {
		return "60s"
	}

	secs := (d + time.Second - 1) / time.Second
	if secs < 1 {
		secs = 1
	}

	return fmt.Sprintf("%ds", secs)
}

// refusedDial reports whether a failed dial was the peer saying no rather than
// the host not being there yet. A retry that meets a refusal stops: the same
// question would get the same answer (#326).
//
// The end-to-end layer names the failures of its own that are answers; the
// transport and the pin stores add the ones they own.
func refusedDial(err error) bool {
	return e2e.IsRefusal(err) ||
		errors.Is(err, websocketcore.ErrRefused) ||
		errors.Is(err, discovery.ErrIdentityChanged) ||
		errors.Is(err, discovery.ErrCertificateChanged)
}

// toggleHidden turns the composer's secret mode on or off.
//
// While it is on the field is a password entry: the characters are concealed,
// and on a phone the platform's password keyboard is what opens, so a secret is
// not handed to a third-party keyboard either. The eye says which way the next
// press goes (#469).
func (g *gui) toggleHidden() {
	g.hidden = !g.hidden

	g.entry.Password = g.hidden
	g.entry.Refresh()

	if g.hidden {
		g.hideBtn.SetIcon(theme.VisibilityIcon())
		g.hideBtn.SetCopyText("Send normally")
		g.setStatus("Password mode: the message is masked, and arrives masked")

		return
	}

	g.hideBtn.SetIcon(theme.VisibilityOffIcon())
	g.hideBtn.SetCopyText("Send as password")
	g.setStatus("Password mode off")
}

// composerChanged runs on every change of the message field.
//
// A newline appended to the end is Enter, and Enter sends — the behaviour every
// chat app a desktop user knows (#323). Fyne's multiline entry inserts a newline
// for Enter and only reports Shift+Enter through OnSubmitted, so the two are told
// apart here: the appended newline is taken back off and sent, while
// Shift+Enter's newline is inserted by insertComposerNewline and never looks like
// an append.
//
// A phone's keyboard has no Shift: its enter key appends the same newline, so it
// sends too, which is what a phone expects.
func (g *gui) composerChanged(text string) {
	g.growComposer(text)

	// A phone breaks the line on Enter and sends with the button or the
	// keyboard's action key. Inferring "Enter" from an appended newline made the
	// same key send or break depending on a hidden flag, which is the report
	// (#362).
	if g.mobile {
		return
	}

	if g.editingComposer {
		g.lastComposer = text

		return
	}

	if text == g.lastComposer+"\n" {
		g.lastComposer = ""
		g.send(strings.TrimSuffix(text, "\n"))

		return
	}

	g.lastComposer = text
}

// insertComposerNewline puts a line break where the cursor is: what Shift+Enter
// does, and what the field's own Enter used to do.
func (g *gui) insertComposerNewline() {
	entry := g.entry
	if entry == nil {
		return
	}

	lines := strings.Split(entry.Text, "\n")
	row, column := entry.CursorRow, entry.CursorColumn

	if row < 0 || row >= len(lines) {
		lines = append(lines, "")

		row = len(lines) - 2
		column = len([]rune(lines[row]))
	}

	runes := []rune(lines[row])
	if column < 0 || column > len(runes) {
		column = len(runes)
	}

	lines[row] = string(runes[:column]) + "\n" + string(runes[column:])

	g.editingComposer = true
	entry.SetText(strings.Join(lines, "\n"))
	g.editingComposer = false

	entry.CursorRow, entry.CursorColumn = row+1, 0
	entry.Refresh()

	g.lastComposer = entry.Text
}

// growComposer gives the field the rows its text needs, within the ceiling.
//
// Only a change of row count touches the widget: SetMinRowsVisible re-lays the
// field out, and doing that on every keystroke is what makes typing feel heavy.
func (g *gui) growComposer(text string) {
	if g.entry == nil {
		return
	}

	rows := composerRows(text, composerColumns(g.entry.Size().Width))
	if rows == g.composerRows {
		return
	}

	g.composerRows = rows
	g.entry.SetMinRowsVisible(rows)
}

// updateButtons enables what the current state can actually do. Send needs a
// session; Stop also makes sense for a host with nobody connected yet. Host and
// Connect change what this device is, and there can be only one of them — so
// they are off while a host, a session, or a start/stop still owns the port.
func (g *gui) updateButtons() {
	g.mu.Lock()
	hasSession := g.sess != nil
	hasServer := g.server != nil
	reconnecting := g.reconnecting
	busy := g.starting || g.stopping
	hasMessages := len(g.messages) > 0
	g.mu.Unlock()

	if hasSession {
		g.sendBtn.Enable()
		g.attachBtn.Enable()
	} else {
		g.sendBtn.Disable()
		g.attachBtn.Disable()
	}

	// Copy all and Select act on the transcript, not on the connection: they
	// work while offline, and only a conversation with something in it gives
	// them something to act on (#469).
	if g.clearAllBtn != nil {
		g.mu.Lock()
		empty := len(g.messages) == 0
		g.mu.Unlock()

		if empty {
			g.clearAllBtn.Disable()
		} else {
			g.clearAllBtn.Enable()
		}
	}

	if g.copyAllBtn != nil {
		if hasMessages {
			g.copyAllBtn.Enable()
		} else {
			g.copyAllBtn.Disable()
		}
	}

	if g.selectBtn != nil {
		if hasMessages {
			g.selectBtn.Enable()
		} else {
			g.selectBtn.Disable()
		}
	}

	// Stop is how a user ends a retry loop as well as a session: without it
	// the only way out of "reconnecting in 60s" would be to close the window.
	if hasSession || hasServer || reconnecting {
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
	fyne.Do(func() {
		// The line exists only while it has something to say: a sentence that
		// stays, or a state standing in it, was the report (#364, #433).
		if text == "" {
			g.status.SetText("")
			g.status.Hide()

			return
		}

		g.status.SetText(text)
		g.status.SetImportance(widget.MediumImportance)
		g.status.Show()
	})
}

// stateGlyphFor is the icon that stands for a connection state, and the colour
// it takes when the platform's font has no emoji to draw (#453).
//
// The order is the order that matters: a retry in flight outranks the session
// that dropped, and a host outranks a client.
func stateGlyphFor(starting, reconnecting, hosting, connected, lost bool) (string, string, fyne.ThemeColorName) {
	switch {
	case starting:
		return "🟡", "starting", theme.ColorNameWarning
	case reconnecting:
		return "🟠", "reconnecting", theme.ColorNameWarning
	case hosting:
		return "🟢", "hosting", theme.ColorNameSuccess
	case connected:
		return "🔵", "connected", theme.ColorNamePrimary
	case lost:
		return "🔴", "lost", theme.ColorNameError
	}

	return "⚪", "offline", theme.ColorNameDisabled
}

// refreshStateGlyph redraws the header's icon from the window's state.
func (g *gui) refreshStateGlyph() {
	if g.stateGlyph == nil {
		return
	}

	g.mu.Lock()
	starting, reconnecting := g.starting, g.reconnecting
	hosting, connected := g.server != nil, g.sess != nil
	lost := g.lost
	g.mu.Unlock()

	glyph, word, colour := stateGlyphFor(starting, reconnecting, hosting, connected, lost)

	g.stateGlyph.Text = glyph
	g.stateGlyph.Color = theme.Color(colour)
	g.stateGlyph.Refresh()

	g.stateWord.SetText(word)
}

// addMessage appends a line to the transcript and scrolls to it.
//
// The list reads g.messages by index, so one Refresh is enough: the row count,
// the rows' text and the drop that keeps the log at maxMessages all follow from
// the slice.
func (g *gui) addMessage(m chatMessage) {
	fyne.Do(func() {
		var started time.Time

		if g.perfEnabled {
			started = time.Now()
		}

		// Whether to follow is decided before the message is added: it is the
		// reader's position that decides, not the row's (#434).
		follow := g.atBottom()

		// The lock is what a reader on another goroutine reads under — the
		// tests, and any helper that inspects the transcript. Painting happens
		// on this goroutine anyway, so the cost is a mutex nobody contends for.
		g.mu.Lock()
		g.messages = appendMessage(g.messages, m)
		g.mu.Unlock()

		g.chatList.Refresh()
		g.updateButtons()

		if follow {
			g.scrollToLatest()
		} else {
			g.jumpBtn.Show()
		}

		if g.perfEnabled {
			if line := g.noteMessageCost(time.Since(started)); line != "" {
				logEvent(line)
			}
		}
	})
}

// perfEvery is how many messages one perf line covers.
const perfEvery = 25

// noteMessageCost records one message's cost and returns the line to log when a
// block of them has been measured, or "" (#432).
//
// It is called on the UI goroutine, from addMessage, and only while
// CTHRONE_PERF is set.
func (g *gui) noteMessageCost(cost time.Duration) string {
	g.perfCount++
	g.perfTotal += cost
	g.perfLast = cost

	if g.perfCount%perfEvery != 0 {
		return ""
	}

	g.mu.Lock()
	rows := len(g.messages)
	g.mu.Unlock()

	// A cost that climbs with the row count is the refresh path; one that stays
	// flat while the window stutters is the renderer. Goroutines and heap are
	// the third answer — something that never stops growing — and the only one
	// that explains "it starts lagging after a while" by itself (#432).
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)

	return fmt.Sprintf("perf: %d messages, %.2f ms each on average, last %.2f ms, %d rows, %d live, %d goroutines, %.1f MB heap",
		g.perfCount,
		float64(g.perfTotal)/float64(g.perfCount)/float64(time.Millisecond),
		float64(g.perfLast)/float64(time.Millisecond),
		rows,
		g.liveSessions,
		runtime.NumGoroutine(),
		float64(memory.HeapAlloc)/(1<<20),
	)
}

// chatOffset is where the transcript is scrolled to. It is a variable so a test
// can put the reader somewhere without a rendered list (#434).
var chatOffset = func(list *widget.List) float32 { return list.GetScrollOffset() }

// atBottom reports whether the transcript is at its end, which is where a new
// message is followed from.
func (g *gui) atBottom() bool {
	if g.chatList == nil {
		return true
	}

	// Before the list has ever been scrolled there is no end to compare with:
	// a fresh transcript follows, which is what a window opening to a room
	// wants.
	if g.lastBottom == 0 {
		return true
	}

	return chatOffset(g.chatList) >= g.lastBottom-2
}

// scrollTick is how often the reader's position is looked at. It only decides
// whether one button is visible, so it can be coarse.
const scrollTick = 200 * time.Millisecond

// trackScroll keeps the way down in step with where the reader is.
//
// Fyne's List keeps its own OnScrolled, so the position is sampled rather than
// pushed: one comparison per tick decides whether the button is shown, and the
// largest offset seen is remembered as the end, so atBottom is right after a
// manual scroll too (#508).
func (g *gui) trackScroll() {
	if g.chatList == nil {
		return
	}

	offset := chatOffset(g.chatList)

	if g.jumpBtn == nil {
		return
	}

	if offset < g.lastBottom-2 {
		g.jumpBtn.Show()

		return
	}

	g.jumpBtn.Hide()
}

// watchScroll samples the transcript's position until the window closes.
func (g *gui) watchScroll() {
	ticker := time.NewTicker(scrollTick)
	defer ticker.Stop()

	for {
		select {
		case <-g.scrollStop:
			return
		case <-ticker.C:
			fyne.Do(g.trackScroll)
		}
	}
}

// scrollToLatest takes the transcript to its end and remembers where that is.
func (g *gui) scrollToLatest() {
	if g.chatList == nil {
		return
	}

	g.chatList.ScrollToBottom()
	g.lastBottom = chatOffset(g.chatList)

	if g.jumpBtn != nil {
		g.jumpBtn.Hide()
	}
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

	caption, _ := row.Objects[0].(*fyne.Container)
	pic, _ := row.Objects[1].(*canvas.Image)
	preview, _ := row.Objects[2].(*widgets.Text)
	save, _ := row.Objects[3].(*widgets.CopyButton)
	open, _ := row.Objects[4].(*widgets.CopyButton)

	// The player is the sixth object, and optional: a row built before it was
	// added still paints as the file row it was.
	var audio audioRow
	if len(row.Objects) > 5 {
		audio, _ = audioRowOf(row.Objects[5])
	}

	var video videoRow
	if len(row.Objects) > 6 {
		video, _ = videoRowOf(row.Objects[6])
	}

	// The picture's copy action is the last object, so the file actions above
	// keep their positions (#510).
	var copyPic *widgets.CopyButton
	if len(row.Objects) > 7 {
		copyPic, _ = row.Objects[7].(*widgets.CopyButton)
	}

	var label *widgets.Text
	var icon *widget.Icon
	var sender *widgets.Text
	var reveal *widgets.CopyButton
	var copyMessage *widgets.CopyButton

	// The caption cell holds the file-type icon on its border and, in the middle,
	// a box of the sender's line over the message. That line is itself a box:
	// the name, a spacer, then the eye and the copy button, in that order. The
	// eye and the copy button are told apart by where the template puts them,
	// which a horizontal box keeps; a border does not (#496, #503).
	if caption != nil {
		for _, object := range caption.Objects {
			switch part := object.(type) {
			case *widget.Icon:
				icon = part
			case *fyne.Container:
				for _, inner := range part.Objects {
					switch piece := inner.(type) {
					case *widgets.Text:
						label = piece
					case *fyne.Container:
						for _, item := range piece.Objects {
							switch child := item.(type) {
							case *widgets.Text:
								sender = child
							case *widgets.CopyButton:
								if reveal == nil {
									reveal = child
								} else {
									copyMessage = child
								}
							}
						}
					}
				}
			}
		}
	}

	if label == nil || icon == nil || sender == nil || reveal == nil || copyMessage == nil || pic == nil || preview == nil || save == nil || open == nil {
		return
	}

	m := g.messages[id]
	g.setSenderLine(sender, &m, g.sel.contains(id))
	label.SetText(m.body())
	label.SetOnTapped(func() { g.selectChatRow(id) })
	copyMessage.SetOnTapped(func() { g.copyRow(id) })
	g.setReveal(reveal, id, m)
	if copyPic != nil {
		copyPic.SetOnTapped(func() { g.copyPicture(id) })
	}

	g.fillFileRow(m, pic, preview, icon, save, open, copyPic)
	g.fillAudioRow(m, audio)
	g.fillVideoRow(m, video)

	width := row.Size().Width
	if g.chatList == nil || width <= 0 {
		return
	}

	row.Resize(fyne.NewSize(width, row.MinSize().Height))
	g.chatList.SetItemHeight(id, row.MinSize().Height)
}

// setReveal binds the eye on a hidden message's row: reveal it while masked,
// hide it again once revealed. A message that was never hidden has no control —
// there is nothing to uncover, and masking after the fact is not offered (#496,
// #503).
//
// The icon is set only when it changes. `SetIcon` re-themes the resource, and
// theming an SVG parses and rasterises it, so calling it on every repaint of
// every visible row cost more than the rest of the message path together —
// 0.73 of 1.05 ms, measured (#432, #491).
//
// The comparison is by identity, not by name: a theme hands out one resource
// per icon, so a theme that changes the eye's shape is a different resource and
// is applied, while a repaint in the same theme is not. A theme that builds a
// new resource on every call simply gets the old behaviour.
func (g *gui) setReveal(button *widgets.CopyButton, id widget.ListItemID, m chatMessage) {
	if button == nil {
		return
	}

	if !m.hidden {
		button.Hide()

		return
	}

	button.Show()
	button.SetCopyText("")

	icon := theme.VisibilityOffIcon()
	if m.masked() {
		icon = theme.VisibilityIcon()
	}

	if current := button.Icon(); current != icon {
		button.SetIcon(icon)
	}

	if m.masked() {
		button.SetOnTapped(func() { g.revealMessage(id) })

		return
	}

	button.SetOnTapped(func() { g.hideMessage(id) })
}

// hideMessage masks one row on this device. It is local: a received message is
// not recalled from the sender, and the other device is unchanged (#474).
func (g *gui) hideMessage(id widget.ListItemID) {
	g.mu.Lock()
	if id < 0 || id >= len(g.messages) {
		g.mu.Unlock()

		return
	}

	g.messages[id].revealed = false
	g.mu.Unlock()

	g.chatList.RefreshItem(id)
}

// copyPicture puts a row's picture on the system clipboard, as a picture.
//
// The work is the platform's and may take a moment — encoding, then handing the
// bytes to a tool — so it happens off the UI goroutine and the answer lands on
// the status line (#510).
func (g *gui) copyPicture(id widget.ListItemID) {
	g.mu.Lock()
	if id < 0 || id >= len(g.messages) {
		g.mu.Unlock()

		return
	}

	message := g.messages[id]
	g.mu.Unlock()

	if message.file == nil || message.file.path == "" {
		return
	}

	path := message.file.path

	go fyne.Do(func() { g.copyPictureNow(path) })
}

// copyPictureNow does the copy and says how it went. It is the body of
// copyPicture, split out so a test can call it without a goroutine.
func (g *gui) copyPictureNow(path string) {
	if err := copyImageToOS(path); err != nil {
		g.setStatus("Could not copy the picture: " + err.Error())

		return
	}

	g.setStatus("Picture copied to the clipboard")
}

// copyRow puts one message on the clipboard, from the copy button on its row.
// The tap on a row no longer copies: it is a selection gesture, and a copy is
// something the reader asks for (#496).
func (g *gui) copyRow(id widget.ListItemID) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	g.mu.Lock()
	message := g.messages[id]
	g.mu.Unlock()

	g.copyToClipboard(message.copyText())
}

// revealMessage shows a masked row again, on this device only.
func (g *gui) revealMessage(id widget.ListItemID) {
	g.mu.Lock()
	if id < 0 || id >= len(g.messages) {
		g.mu.Unlock()

		return
	}

	g.messages[id].hidden = true
	g.messages[id].revealed = true
	g.mu.Unlock()

	g.chatList.RefreshItem(id)
	g.setStatus("Message revealed")
}

// selectChatRow is the row's gesture: it marks a range while one is being
// built, opens a picture, and otherwise does nothing but put the keyboard away.
//
// It used to copy the message (#469). A copy is now asked for — the row's copy
// button, the header's Copy all, or selecting the text itself — so a tap on a
// message is a place to start a selection and nothing else (#496).
func (g *gui) selectChatRow(id widget.ListItemID) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	// On a phone the keyboard covers the bottom of the screen and the window
	// resizes under it, so the tap meant for Send can land on a row. While the
	// composer holds the keyboard, a tap on the transcript only puts it away, as
	// it has since #325.
	if g.mobile && g.composerHasKeyboard() {
		g.win.Canvas().Unfocus()

		return
	}

	g.chatList.Unselect(id)

	if g.sel.active {
		g.tapSelection(id)

		return
	}

	message := g.messages[id]
	if message.file != nil && message.file.inline && message.file.path != "" {
		g.showImage(message.file.path)
	}
}

// beginSelection enters the range-copy mode: the next transcript tap marks one
// end of the span and the one after it marks the other.
func (g *gui) beginSelection() {
	g.mu.Lock()
	empty := len(g.messages) == 0
	g.mu.Unlock()

	if empty {
		return
	}

	g.sel = selection{active: true, anchor: noSelection, focus: noSelection}
	g.showSelectionHeader(true)
	g.updateSelectionHeader()
	g.setStatus("Tap the first message, then the last")
}

// endSelection leaves range mode and puts the chat header back.
func (g *gui) endSelection() {
	if !g.sel.active {
		return
	}

	g.sel = selection{anchor: noSelection, focus: noSelection}
	g.showSelectionHeader(false)
	g.chatList.Refresh()
}

// showSelectionHeader swaps the state row for the selection actions, or back.
func (g *gui) showSelectionHeader(show bool) {
	if g.headerHolder == nil || g.selectHeader == nil || len(g.headerHolder.Objects) < 2 {
		return
	}

	state := g.headerHolder.Objects[0]

	if show {
		state.Hide()
		g.selectHeader.Show()
	} else {
		g.selectHeader.Hide()
		state.Show()
	}
}

// updateSelectionHeader says how many rows the span covers and which actions
// have something to act on.
func (g *gui) updateSelectionHeader() {
	if g.selCount == nil {
		return
	}

	count := g.sel.count()
	g.selCount.SetText(fmt.Sprintf("%d selected", count))

	for _, button := range []*widgets.CopyButton{g.selCopyBtn, g.selDeleteBtn} {
		if button == nil {
			continue
		}

		if count > 0 {
			button.Enable()
		} else {
			button.Disable()
		}
	}
}

// tapSelection marks one end of the span. The first tap sets both ends, so a
// single message can be copied or deleted without a second tap.
func (g *gui) tapSelection(id int) {
	if id < 0 || id >= len(g.messages) {
		return
	}

	if g.sel.anchor == noSelection {
		g.sel.anchor = id
		g.setStatus("Tap the last message")
	}

	g.sel.focus = id
	g.updateSelectionHeader()
	g.chatList.Refresh()
}

// selectionSpan is the range the header's actions apply to, clamped to the log.
func (g *gui) selectionSpan() (int, int, bool) {
	g.mu.Lock()
	count := len(g.messages)
	g.mu.Unlock()

	if !g.sel.active || count == 0 {
		return 0, 0, false
	}

	start := g.sel.start()
	if start == noSelection || start >= count {
		return 0, 0, false
	}

	end := g.sel.end()
	if end >= count {
		end = count - 1
	}

	return start, end, true
}

// copySelection puts the selected span on the clipboard, one message per line.
func (g *gui) copySelection() {
	start, end, ok := g.selectionSpan()
	if !ok {
		return
	}

	g.mu.Lock()
	text := transcriptOf(g.messages[start : end+1])
	g.mu.Unlock()

	g.copyToClipboard(text)
	g.setStatus(fmt.Sprintf("Copied %d message(s)", end-start+1))
	g.endSelection()
}

// copyAll puts the whole transcript on the clipboard in order, sender included.
func (g *gui) copyAll() {
	g.mu.Lock()
	text := transcriptOf(g.messages)
	count := len(g.messages)
	g.mu.Unlock()

	if text == "" {
		return
	}

	g.copyToClipboard(text)
	g.setStatus(fmt.Sprintf("Copied %d message(s)", count))
}

// deleteSelection removes the selected span from this device.
func (g *gui) deleteSelection() {
	start, end, ok := g.selectionSpan()
	if !ok {
		return
	}

	g.removeMessages(start, end)
}

// removeMessages takes a span out of the transcript, and with it the app's own
// unsaved copies of any files in it. Nothing is sent to the other device: this
// is the local transcript, not the conversation (#469).
func (g *gui) removeMessages(start, end int) {
	g.mu.Lock()
	count := len(g.messages)

	notes := make([]*fileNote, 0, end-start+1)
	for i := start; i <= end && i < count; i++ {
		if g.messages[i].file != nil {
			notes = append(notes, g.messages[i].file)
		}
	}

	g.messages = removeRange(g.messages, start, end)
	g.mu.Unlock()

	for _, note := range notes {
		g.discardStaged(note)
	}

	g.sel = selection{anchor: noSelection, focus: noSelection}
	g.showSelectionHeader(false)
	g.chatList.Refresh()
	g.updateButtons()
	g.setStatus(fmt.Sprintf("Deleted %d message(s) from this device", end-start+1))
}

// askClear is the question before the transcript is emptied.
//
// It is a variable so a test can answer without a dialog, the same seam
// install.go uses for its confirmations.
var askClear = func(g *gui, count int, answer func(bool)) {
	g.showCopyableConfirm(
		"Clear all messages",
		fmt.Sprintf("Clear all %d messages on this device?\n\nThey are not recalled from the other device, and files already saved are kept.", count),
		"Clear", "Cancel",
		answer,
	)
}

// clearTranscript empties the transcript, after asking: it is the "start clean"
// action, and the one delete that can take a whole conversation with it.
func (g *gui) clearTranscript() {
	g.mu.Lock()
	count := len(g.messages)

	notes := make([]*fileNote, 0, count)
	for _, message := range g.messages {
		if message.file != nil {
			notes = append(notes, message.file)
		}
	}
	g.mu.Unlock()

	if count == 0 {
		return
	}

	askClear(g, count, func(ok bool) {
		if !ok {
			return
		}

		g.cancelTransfers()
		g.abandonIncoming()

		g.mu.Lock()
		g.messages = nil
		g.mu.Unlock()

		for _, note := range notes {
			g.discardStaged(note)
		}

		g.sel = selection{anchor: noSelection, focus: noSelection}
		g.showSelectionHeader(false)
		g.chatList.Refresh()
		g.updateButtons()
		g.setStatus("Transcript cleared on this device")
	})
}

// discardStaged removes the app's own unsaved copy behind a row, and only that.
//
// A row waiting to be saved points into the staging folder; deleting the row
// should not leave the bytes there for a message nobody can reach. A saved row
// points at the user's folder, and a sent row at the user's own file, and
// neither is this function's to remove (#469).
func (g *gui) discardStaged(note *fileNote) {
	if note == nil || note.path == "" || note.state != transferPending {
		return
	}

	root, err := filepath.Abs(g.stagingDir())
	if err != nil {
		return
	}

	path, err := filepath.Abs(note.path)
	if err != nil {
		return
	}

	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return
	}

	_ = os.Remove(path)
}

// abandonIncoming marks every arrival still being assembled, so the receive
// loop drops it instead of adding a row to a transcript that was just cleared.
func (g *gui) abandonIncoming() {
	g.mu.Lock()
	defer g.mu.Unlock()

	for _, entry := range g.incoming {
		if entry != nil {
			entry.abandoned = true
		}
	}
}

// composerHasKeyboard reports whether the message field is the focused object,
// which is when a phone's keyboard is up for it.
func (g *gui) composerHasKeyboard() bool {
	if g.entry == nil || g.win == nil || g.win.Canvas() == nil {
		return false
	}

	focused := g.win.Canvas().Focused()

	return focused == fyne.Focusable(g.entry)
}

// copyToClipboard puts text on the clipboard and confirms it on screen.
func (g *gui) copyToClipboard(text string) {
	if g.app == nil || text == "" {
		return
	}

	g.app.Clipboard().SetContent(text)

	// One confirmation for the whole window: the header no longer has a line of
	// its own for it, and the popup was already doing this on every other tab
	// (#383).
	g.showCopied()
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
				// The Devices list already shows it; the chat's line is for
				// what happened in the conversation (#433).
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

	g.aboutCredential = g.text("")
	g.aboutNote = g.text("")

	// The name is the heading and the version sits under it, rather than the
	// two sharing one line: at phone width "cthrone v0.8.3" in a horizontal box
	// wraps mid-word or pushes the version off the edge, and a header that
	// changes shape with the window is the thing that looked broken.
	header := g.aboutHeader(version)

	repoBtn := widgets.NewButtonWithIcon("Open", theme.InfoIcon(), g.openRepository, g.copyLabel)
	repoLine := container.NewBorder(nil, nil, nil, repoBtn, g.text(RepositoryURL))

	g.updateBtn = widgets.NewButtonWithIcon("Check for updates", theme.ViewRefreshIcon(), g.checkUpdates, g.copyLabel)

	g.releaseBtn = widgets.NewButtonWithIcon("Release page", theme.DownloadIcon(), g.openReleasePage, g.copyLabel)

	// The install and the way back start hidden: one appears when a check finds
	// a newer release, the other when there is a previous binary beside this
	// one. A button that can only fail reads as a broken feature.
	g.installBtn = widgets.NewButtonWithIcon("Install", theme.DownloadIcon(), g.installRelease, g.copyLabel)
	g.installBtn.Hide()

	// The way back has two forms, and they are different questions. Revert puts
	// back the binary an install replaced; "previous release" fetches the one
	// published before this from the release page, which is the only route for
	// a build that arrived by unpacking the archive (#384).
	g.previousBtn = widgets.NewButtonWithIcon("Install previous release", theme.ContentUndoIcon(), g.installPreviousRelease, g.copyLabel)
	g.previousBtn.Hide()

	g.restartBtn = widgets.NewButtonWithIcon("Restart", theme.MediaReplayIcon(), g.restartAfterInstall, g.copyLabel)
	g.restartBtn.Hide()

	g.revertBtn = widgets.NewButtonWithIcon("Revert to previous", theme.ContentUndoIcon(), g.revertRelease, g.copyLabel)
	g.revertBtn.Hide()

	g.wikiBtn = widgets.NewButtonWithIcon("Wiki", theme.HelpIcon(), g.openWiki, g.copyLabel)

	noteText := "Nothing is downloaded and nothing is installed: the check only reports the version, and the release page does the rest."
	if !selfInstallable() {
		noteText = appInstallsFromTheReleasePage
	}

	note := g.label(noteText)

	body := container.NewVBox(
		header,
		repoLine,
		widget.NewSeparator(),
		g.aboutCredential,
		widget.NewSeparator(),
		g.updateBtn,
		g.aboutNote,
		g.installBtn,
		g.previousBtn,
		g.restartBtn,
		g.revertBtn,
		note,
		g.releaseBtn,
		g.wikiBtn,
	)

	// The credential line was filled in before this tab existed, so it is
	// brought up to date now rather than staying empty until the next rebuild.
	g.refreshCredential()
	g.refreshRevert()
	g.refreshPrevious()

	return container.NewVScroll(container.NewPadded(body))
}

// aboutHeader is the app's mark, its name and the version it is running.
//
// The logo sits at the left of the name, and the version at the right of the
// same line where the width allows it — a desktop has room, and a phone does
// not, so a phone drops the version under the name instead of squeezing it. The
// icon is the one the window and the APK carry, not a second copy.
func (g *gui) aboutHeader(version string) fyne.CanvasObject {
	// The version is part of the name's own line rather than a second object
	// beside it: a box of two texts can wrap or clip on a narrow phone, and the
	// version was reported as sitting above the name (#343, #364).
	//
	// It is text a user pastes into a report, so it can be selected: on a phone
	// that needs a long press, and a long press only reaches a text that has a
	// gesture layer (#497).
	name := g.textHeading("cthrone  " + version)
	name.AllowSelection()

	return container.NewHBox(
		widget.NewIcon(appIcon()),
		name,
	)
}

// openRepository opens the project's page in the browser.
//
// A link that does nothing is the failure this reports: `OpenURL` returns the
// reason a desktop has no browser or a phone has no handler, and a swallowed
// one leaves the user clicking a dead button.
func (g *gui) openRepository() {
	g.openPage(RepositoryURL)
}

// openWiki opens the walk-through: one page per task, for the reader the README
// is too long for. Its source is in docs/wiki, reviewed with the code.
func (g *gui) openWiki() {
	g.openPage(WikiURL)
}

// openReleasePage opens the release page: the list the update check's answer
// points at, so a user who has just been told a newer release exists can reach
// it without going to find the repository first.
func (g *gui) openReleasePage() {
	g.openPage(ReleasesURL)
}

// openPage opens an address in the platform's browser, reporting a failure.
//
// The opener is a variable so a test can see which address a button asked for:
// the failure this guards against is a button wired to the wrong URL, which
// looks identical to a working one until somebody taps it.
var openPageInBrowser = func(app fyne.App, link *url.URL) error { return app.OpenURL(link) }

func (g *gui) openPage(address string) {
	link, err := url.Parse(address)
	if err != nil {
		g.showError(err)

		return
	}

	if err := openPageInBrowser(g.app, link); err != nil {
		g.showError(fmt.Errorf("could not open %s: %w", address, err))
	}
}

// updateCacheWindow is how long one check's answer is reused. The GitHub API
// allows 60 requests an hour per address, so pressing the button repeatedly used
// to spend a shared budget and then read 403 (#322). The release page's redirect
// is not rate limited, and this keeps even that to one request per window.
const updateCacheWindow = 10 * time.Minute

// checkUpdates asks GitHub for the newest release, off the UI goroutine.
//
// The answer is always written back through fyne.Do, including the failures: a
// button left disabled or a note left saying "Checking…" is the spinner that
// never resolves this tab is built to avoid.
var checkReleaseURL = updatepkg.LatestReleaseURL

func (g *gui) checkUpdates() {
	// A check made a moment ago is the answer: pressing again is not a new
	// question, and every request costs against a limit shared by the address
	// (#322).
	g.mu.Lock()
	cached, when := g.updateTag, g.updateAt
	g.mu.Unlock()

	if cached != "" && time.Since(when) < updateCacheWindow {
		current := aboutVersion(Version, g.app.Metadata().Version)
		g.setAboutNote(aboutUpdateText(current, cached))
		g.offerNewer(current, cached)

		return
	}

	if g.updateBtn != nil {
		g.updateBtn.Disable()
	}

	g.setAboutNote("Checking…")

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updatepkg.CheckTimeout)
		defer cancel()

		tag, err := updatepkg.LatestTag(ctx, &http.Client{Timeout: updatepkg.CheckTimeout}, checkReleaseURL)

		fyne.Do(func() {
			if g.updateBtn != nil {
				g.updateBtn.Enable()
			}

			if err != nil {
				g.setAboutNote("Could not check: " + err.Error() + ".")

				return
			}

			g.mu.Lock()
			g.updateTag, g.updateAt = tag, time.Now()
			g.mu.Unlock()

			current := aboutVersion(Version, g.app.Metadata().Version)
			g.setAboutNote(aboutUpdateText(current, tag))
			g.offerNewer(current, tag)
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
