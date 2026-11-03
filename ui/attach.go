//go:build gui

package ui

import (
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"

	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/fiwon123/cthrone/internal/wire"
	"github.com/fiwon123/cthrone/ui/widgets"
)

// outgoing is a picked file waiting its turn on the transport.
//
// The staged copy is the path: staging exists because a picker hands over a
// stream with no length, and every header needs the length before the first
// chunk. The row is a pointer, so a progress update finds the row the file was
// given without keeping an index that a trimming log would invalidate.
type outgoing struct {
	id   string
	name string
	mime string
	path string
	size int64
	note *fileNote
}

// incomingTransfer is a file being received, and the row that reports it.
//
// received is only touched by the session's read goroutine; the note's own
// fields are only written inside fyne.Do, because the list reads them while it
// paints.
type incomingTransfer struct {
	note     *fileNote
	from     string
	received int64

	// abandoned is a cancel that arrived while chunks were still in flight. The
	// receive loop reads it and drops the transfer, because the assembler
	// belongs to that goroutine and not to the one the button was pressed on.
	abandoned bool
}

// pickFile asks for a file and queues it for the live session.
func (g *gui) pickFile() {
	g.mu.Lock()
	sess := g.sess
	g.mu.Unlock()

	if sess == nil {
		g.setStatus("Connect before attaching a file")

		return
	}

	picker := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			g.showError(err)

			return
		}

		if reader == nil {
			// The user closed the picker: nothing was picked, which is not an
			// error and needs no row.
			return
		}

		g.rememberPick(reader)
		g.queuePick(reader)
	}, g.win)
	picker.Resize(g.dialogSize())

	// Open where the user last picked from, or in the folder received files
	// land in — not in the toolkit's default list of places. On Windows that
	// list is the home folder and its Quick Access, which can be OneDrive or a
	// disconnected share: enumerating it is what made Attach take seconds
	// (#386).
	if start := g.pickStart(); start != nil {
		picker.SetLocation(start)
	}

	picker.Show()
}

// pickStart is the folder the file picker opens in: the one the user last
// picked from, else the folder received files land in, else the home folder.
//
// It is a folder this program already knows, not the toolkit's list of places:
// the point is that opening the picker does not walk anything slow — on
// Windows that list can be the home folder's Quick Access, OneDrive, or a
// share that is no longer connected (#386). A directory that is not there is
// skipped rather than opened.
func (g *gui) pickStart() fyne.ListableURI {
	home, _ := os.UserHomeDir()

	for _, dir := range []string{g.lastPick, g.downloadDir(), home} {
		if dir == "" {
			continue
		}

		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			continue
		}

		uri, err := storage.ListerForURI(storage.NewFileURI(dir))
		if err != nil {
			continue
		}

		return uri
	}

	return nil
}

// folderStart is dir as a picker location, or nil when it is not there.
//
// A file dialog opens here rather than at the toolkit's place list: the folder
// the user chose for received files is the one a save should start from (#469).
func (g *gui) folderStart(dir string) fyne.ListableURI {
	if dir == "" {
		return nil
	}

	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return nil
	}

	uri, err := storage.ListerForURI(storage.NewFileURI(dir))
	if err != nil {
		return nil
	}

	return uri
}

// rememberPick records the folder a pick came from, so the next one opens
// there. A phone's content:// URI has no folder to remember.
func (g *gui) rememberPick(reader fyne.URIReadCloser) {
	uri := reader.URI()
	if uri == nil || uri.Scheme() != "file" {
		return
	}

	if dir := filepath.Dir(uri.Path()); dir != "." && dir != "" {
		g.lastPick = dir
	}
}

// sourcePath is the picked file on this device's disk, or "" when the picker
// handed over something that is not a path — a phone's content URI, for one.
//
// It is only ever read for the row's preview: the bytes that travel are read
// from the staged copy, because the picker's reader is consumed once.
func sourcePath(reader fyne.URIReadCloser) string {
	uri := reader.URI()
	if uri == nil || uri.Scheme() != "file" {
		return ""
	}

	return uri.Path()
}

// stageForSend copies a picked file into the app's staging folder, the one the
// receive side already uses.
//
// It used to stage into the download folder, which is where the user's own files
// live and has nothing to do with a temporary copy (#303).
func (g *gui) stageForSend(r io.Reader) (string, int64, error) {
	dir, err := g.ensureStagingDir()
	if err != nil {
		return "", 0, err
	}

	return stage(r, dir)
}

// queuePick stages the picked file and hands it to the sender.
//
// The copy is made off the UI goroutine: a 200 MB video takes a moment to
// duplicate, and doing it where the window's events are handled is the freeze
// this row-per-transfer design exists to avoid.
func (g *gui) queuePick(reader fyne.URIReadCloser) {
	name := "file"
	if uri := reader.URI(); uri != nil && uri.Name() != "" {
		name = uri.Name()
	}

	g.setStatus("Preparing " + name + "…")

	// The source path is read before the goroutine consumes the picker, and it
	// is empty when the picker gave a content:// URI — a phone — which is what
	// the row's thumbnail is for.
	source := sourcePath(reader)

	go func() {
		defer reader.Close()

		path, size, err := g.stageForSend(reader)
		if err != nil {
			// A row that says the pick failed is the difference between "my
			// picture did not send" with something to look at and nothing at
			// all (#303).
			fyne.Do(func() {
				g.addMessage(chatMessage{file: &fileNote{name: name, state: transferFailed}})
			})
			g.setStatus("Attach failed: " + err.Error())

			return
		}

		// A picker may name the file with a document id and no extension — the
		// content:// case — so the type is read from the bytes when the name
		// cannot say it, and the name gets a suffix the receiver can act on
		// (#303).
		mime := typeOf(path, name)
		name = withExtension(name, mime)

		// The row draws the same preview the peer will draw (#237). The file it
		// reads from is the user's own, not the staged copy: the copy is removed
		// as soon as the send returns, and a row pointing at it would lose its
		// picture the moment the transfer finished.
		//
		// A picker that gave no path leaves nothing to read later, so the
		// picture is reduced now, while the staged copy exists (#303).
		inline := showsInline(mime, size)
		note := &fileNote{
			id:     newTransferID(),
			name:   name,
			size:   size,
			state:  transferSending,
			path:   source,
			inline: inline,
		}

		if inline && source == "" {
			note.thumb = thumbnailPNG(path, inlineImageHeight)
		}

		item := outgoing{
			id:   note.id,
			name: name,
			mime: mime,
			path: path,
			size: size,
			note: note,
		}

		fyne.Do(func() { g.addMessage(chatMessage{file: note}) })
		g.outbox <- item
	}()
}

// pumpOutbox sends picked files one at a time, in the order they were picked.
//
// One worker is the whole queueing rule: two files picked in a row become two
// transfers, not one interleaved stream whose chunks the receiver would reject.
func (g *gui) pumpOutbox() {
	for item := range g.outbox {
		g.sendTransfer(item)
	}
}

// sendTransfer publishes one staged file and reports it in its row.
func (g *gui) sendTransfer(item outgoing) {
	// The staged copy has no other purpose once this call returns, whatever
	// the outcome: cancelled, failed, or sent.
	defer os.Remove(item.path)

	sender, err := g.outgoing()
	if err != nil {
		g.updateTransfer(item.note, transferFailed)
		g.setStatus("Nothing to send to — " + err.Error())

		return
	}

	file, err := os.Open(item.path)
	if err != nil {
		g.updateTransfer(item.note, transferFailed)
		g.showError(err)

		return
	}
	defer file.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	g.trackTransfer(item.id, cancel)

	err = sendFile(ctx, sender, item.id, item.name, item.mime, item.size, file, func(sent int64) {
		g.updateProgress(item.note, sent)
	})

	g.untrackTransfer(item.id)

	switch {
	case err == nil:
		g.updateTransfer(item.note, transferSent)

	case errors.Is(err, context.Canceled):
		// Tell the other side rather than leaving it holding a half file until
		// its assembler times out. Best effort: a session that has gone away
		// carries nothing, and the timeout is still the backstop.
		g.sendAbort(sender, item.id)
		g.removeTransfer(item.note)
		g.setStatus("Sending " + item.name + " cancelled")

	default:
		g.updateTransfer(item.note, transferFailed)
		g.setStatus("Sending " + item.name + " failed: " + err.Error())
	}
}

// sendAbort tells the peer that a transfer will send no more bytes.
func (g *gui) sendAbort(sender chunkSender, id string) {
	frame, err := wire.Encode(wire.Header{Kind: wire.KindAbort, ID: id}, nil)
	if err != nil {
		return
	}

	_ = sender.Send(frame)
}

// receiveAbort is the other end of sendAbort: the sender gave up, so what was
// being assembled is dropped and the row goes.
//
// It runs on the receive goroutine, which is the one that owns the assembler.
func (g *gui) receiveAbort(from, id string) {
	g.mu.Lock()
	entry := g.incoming[id]
	delete(g.incoming, id)
	assembler := g.assembler
	g.mu.Unlock()

	if assembler != nil {
		assembler.Abandon(id)
	}

	if entry == nil {
		return
	}

	g.removeTransfer(entry.note)

	name := entry.note.name
	if name == "" {
		name = "the file"
	}

	g.setStatus(from + " cancelled " + name)
}

// trackTransfer remembers how to cancel a transfer, so Stop can reach it.
func (g *gui) trackTransfer(id string, cancel context.CancelFunc) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cancels == nil {
		g.cancels = make(map[string]context.CancelFunc)
	}

	g.cancels[id] = cancel
}

func (g *gui) untrackTransfer(id string) {
	g.mu.Lock()
	delete(g.cancels, id)
	g.mu.Unlock()
}

// cancelTransfer stops one transfer, from the row that is showing it.
//
// A send is cancelled through the context its reader is watching. A receive has
// no context to cancel — the bytes are already in flight — so the request is
// recorded on the entry and the receive loop abandons the transfer on the next
// chunk, which is the goroutine that owns the assembler.
func (g *gui) cancelTransfer(id string) {
	if id == "" {
		return
	}

	g.mu.Lock()
	cancel := g.cancels[id]

	entry := g.incoming[id]
	if entry != nil {
		entry.abandoned = true
	}

	g.mu.Unlock()

	if cancel != nil {
		cancel()

		return
	}

	if entry == nil {
		return
	}

	// The row goes now, so the screen agrees with the decision; the part file
	// is removed by the receive loop, or by the assembler's own sweep if no
	// further chunk ever arrives.
	g.removeTransfer(entry.note)
	g.setStatus("Receiving " + entry.note.name + " cancelled")
}

// cancelTransfers stops every transfer on its way. A cancelled transfer leaves
// no file behind: its staged copy is removed by sendTransfer, and a partly
// received one is dropped by the assembler.
func (g *gui) cancelTransfers() {
	g.mu.Lock()
	cancels := make([]context.CancelFunc, 0, len(g.cancels))

	for _, cancel := range g.cancels {
		cancels = append(cancels, cancel)
	}

	g.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// updateTransfer changes a row's state on the UI goroutine.
func (g *gui) updateTransfer(note *fileNote, state transferState) {
	fyne.Do(func() {
		note.state = state
		g.refreshTransfer(note)
	})
}

// removeTransfer takes a cancelled transfer's row out of the transcript.
//
// A cancelled transfer leaves nothing behind on screen either: the user asked
// it to stop, and a row about a file that was never sent is noise. The status
// line says what happened, which is where a transient event belongs.
func (g *gui) removeTransfer(note *fileNote) {
	fyne.Do(func() {
		g.mu.Lock()
		messages, found := dropFile(g.messages, note)
		g.messages = messages
		g.mu.Unlock()

		if found {
			g.chatList.Refresh()
		}
	})
}

// updateProgress moves a row's progress bar text forward.
func (g *gui) updateProgress(note *fileNote, sent int64) {
	fyne.Do(func() {
		note.sent = sent
		g.refreshTransfer(note)
	})
}

// finishTransfer turns a receiving row into the finished one, and hands the file
// to the save policy.
//
// The row is updated from the staged copy first, so a picture can be drawn
// before the file is put away: the verified bytes are all there, only their
// final home is undecided. Manual mode leaves them there and the row offers
// Save as…; only the opt-in automatic mode moves the file into the folder
// (#469).
func (g *gui) finishTransfer(note *fileNote, file *wire.File) {
	settings := g.fileSettings()

	fyne.Do(func() {
		note.name = file.Name
		note.path = file.Path
		note.sent = file.Size
		note.inline = showsInline(file.MIME, file.Size)
		note.state = transferPending
		g.refreshTransfer(note)
	})

	if settings.policy != saveAuto {
		return
	}

	g.saveTo(note, file, settings.folder)
}

// saveTo moves an arrival into folder and points its row at the result.
//
// The move is off the UI goroutine: renaming is cheap, but the copy a folder on
// another filesystem forces is not, and a 200 MB video must not freeze the page
// while it is put away.
func (g *gui) saveTo(note *fileNote, file *wire.File, folder string) {
	target, err := moveInto(file.Path, folder, file.Name)
	if err != nil {
		// The bytes are already verifiable: the assembler checked the size and
		// the SHA-256 before it handed this file over. So a move that fails is
		// a problem with the folder, not with the transfer, and saying "failed"
		// here told the user a file that arrived did not (#286).
		//
		// The row keeps the staged path and offers Save as…, which is the one
		// action that can still put the file somewhere that works.
		fyne.Do(func() {
			note.path = file.Path
			note.sent = file.Size
			note.inline = showsInline(file.MIME, file.Size)
			note.state = transferPending
			g.refreshTransfer(note)
		})
		g.setStatus("Received " + file.Name + ", but could not save it in " + folder + ": " + err.Error())

		return
	}

	fyne.Do(func() {
		note.path = target
		note.state = transferSaved
		g.refreshTransfer(note)
	})
}

// askWhereToSave shows the one dialog ask mode costs per transfer.
//
// Cancelling it is not a refusal: the file stays in the app's own storage and
// the row keeps a Save action, so a file whose dialog was dismissed by accident
// is one click from being saved rather than gone.
func (g *gui) askWhereToSave(note *fileNote, name, staged string) {
	fyne.Do(func() {
		save := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				g.showError(err)
				g.setStatus(name + " is still waiting to be saved")

				return
			}

			if writer == nil {
				g.setStatus(name + " is still waiting to be saved")

				return
			}

			go g.writeStaged(note, name, staged, writer)
		}, g.win)

		save.SetFileName(name)
		save.Resize(g.dialogSize())

		// Open where the file would have gone, so the choice is a folder the
		// user already picked rather than the toolkit's place list (#469).
		if start := g.folderStart(g.fileSettings().folder); start != nil {
			save.SetLocation(start)
		}

		save.Show()
	})
}

// writeStaged copies a staged file into the place the user picked, and drops the
// staged copy once the bytes are there.
func (g *gui) writeStaged(note *fileNote, name, staged string, writer fyne.URIWriteCloser) {
	defer writer.Close()

	if err := copyInto(writer, staged); err != nil {
		g.showError(err)
		g.setStatus("Saving " + name + " failed: " + err.Error())

		return
	}

	os.Remove(staged)

	target := name
	if uri := writer.URI(); uri != nil && uri.Path() != "" {
		target = uri.Path()
	}

	fyne.Do(func() {
		note.path = target
		note.state = transferSaved
		g.refreshTransfer(note)
	})
}

// showFiles is the Files dialog: where received files go, what happens on
// arrival, and where the files of this conversation actually are.
func (g *gui) showFiles() {
	settings := g.fileSettings()

	folder := widget.NewEntry()
	folder.SetText(settings.folder)

	auto := widgets.NewCheckRow("Save received files automatically", nil, !g.mobile)
	auto.SetChecked(settings.policy == saveAuto)

	choose := widgets.NewButtonWithIcon("Choose…", theme.FolderOpenIcon(), func() {
		picker := dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				g.showError(err)

				return
			}

			if uri == nil {
				return
			}

			folder.SetText(uri.Path())
		}, g.win)
		picker.Resize(g.dialogSize())
		picker.Show()
	}, g.copyLabel)

	folderRow := container.NewBorder(g.label("Folder"), nil, nil, choose, folder)

	// The files the conversation has received, and where each one is. The row
	// action copies its path, which is the thing a user needs when the file is
	// already in the app's storage or in a folder they have forgotten (#469).
	received := g.receivedFiles()

	list := container.NewVBox()
	if len(received) == 0 {
		list.Add(g.label("No files received in this conversation yet."))
	} else {
		for _, file := range received {
			list.Add(g.receivedFileRow(file))
		}
	}

	scroll := container.NewVScroll(list)
	scroll.SetMinSize(fyne.NewSize(0, 160))

	body := container.NewVBox(
		g.label("A received file waits in the app until you press Save as… on its row, unless automatic saving is on. Tap a file below to copy its path."),
		folderRow,
		auto,
		widget.NewSeparator(),
		g.heading("Received files"),
		scroll,
	)

	if canOpenFiles() {
		body.Add(widgets.NewButtonWithIcon("Open folder", theme.FolderOpenIcon(), func() {
			dir := expandFolder(folder.Text)
			if dir == "" {
				g.setStatus("Not opened: " + folder.Text + " is not an absolute path")

				return
			}

			g.openPage(storage.NewFileURI(dir).String())
		}, g.copyLabel))
	}

	form := dialog.NewCustomConfirm("Files", "Save", "Cancel", body, func(ok bool) {
		if !ok {
			return
		}

		g.storeFileSettings(folder.Text, auto.Checked())
	}, g.win)
	form.Resize(g.dialogSize())
	form.Show()
}

// receivedFile is one row of the Files dialog: the name, the path a tap copies,
// and the line the row shows.
type receivedFile struct {
	path string
	line string
}

// receivedFiles is what the conversation has received, in transcript order.
//
// It is the rows the reader can still see — the transcript is capped — rather
// than a folder listing: the download folder can hold files this program never
// received, and a file dropped from the capped log has no row left to point at.
func (g *gui) receivedFiles() []receivedFile {
	g.mu.Lock()
	defer g.mu.Unlock()

	files := make([]receivedFile, 0, len(g.messages))

	for _, message := range g.messages {
		note := message.file
		if note == nil || message.from == "" {
			continue
		}

		switch note.state {
		case transferSaved, transferPending, transferFailed:
			files = append(files, receivedFile{path: note.path, line: receivedFileLine(note)})
		}
	}

	return files
}

// receivedFileLine says what the Files dialog needs to say about one arrival:
// the row's own line, with the one case it does not spell out — an unsaved file
// is still in the app's storage.
func receivedFileLine(note *fileNote) string {
	if note.state == transferPending && note.path != "" {
		return fmt.Sprintf("%s (%s) — not saved (kept in the app)", note.name, humanBytes(note.size))
	}

	return note.line()
}

// receivedFileRow is one Files-dialog entry. Tapping it copies the path, which
// is the part a user needs to find the file again.
//
// The row is text rather than a button: a path can be longer than a phone, and
// a button does not wrap while a label does (#469).
func (g *gui) receivedFileRow(file receivedFile) fyne.CanvasObject {
	if file.path == "" {
		return g.label(file.line)
	}

	row := g.text(file.line)
	row.SetOnTapped(func() {
		g.copyToClipboard(file.path)
	})

	return row
}

// storeFileSettings validates and remembers the choice, so it survives a
// restart. A folder that is not a path is refused rather than silently replaced
// by the fallback, because the user meant to change it.
func (g *gui) storeFileSettings(folder string, auto bool) {
	dir := expandFolder(folder)
	if dir == "" {
		g.setStatus("Not saved: " + folder + " is not an absolute path")

		return
	}

	// A folder is remembered only if a file can land in it. The picker on a
	// phone can return a path that is absolute and still is not a directory
	// (#286), and remembering it would fail every later file in silence.
	if err := usableFolder(dir); err != nil {
		g.setStatus("Not saved: " + err.Error())

		return
	}

	settings := fileSettings{folder: dir, policy: saveManual}
	if auto {
		settings.policy = saveAuto
	}

	writeFileSettings(g.preferences(), settings)

	if auto {
		g.setStatus("Files will be saved automatically in " + dir)

		return
	}

	g.setStatus("Files will wait in the app until saved — folder " + dir)
}

// refreshTransfer repaints the row a note belongs to.
//
// The log is capped, so a slow transfer's row can be trimmed out before it
// finishes. A missing row is not an error: the transfer still completes and the
// file still lands; there is simply nothing left on screen to update.
//
// It must run on the UI goroutine: the list is a widget, and the row's state is
// what the row painter reads.
func (g *gui) refreshTransfer(note *fileNote) {
	for id := range g.messages {
		if g.messages[id].file == note {
			g.chatList.RefreshItem(id)

			return
		}
	}
}

// receiveFrame takes one payload from the session: a message, or a piece of a
// file transfer.
//
// Text is exactly the bytes that were sent, so a peer that predates the
// envelope is read as before. Only a payload that starts with the magic is a
// frame, and a frame that does not parse is reported rather than shown as
// text — otherwise a damaged transfer would read as a chat message full of
// binary noise.
func (g *gui) receiveFrame(from string, payload []byte) {
	frame, err := wire.Parse(payload)
	if err != nil {
		g.addMessage(chatMessage{from: from, text: "A damaged file transfer arrived: " + err.Error()})

		return
	}

	// A host relays a message with the sender in the header, so the row names
	// the device that typed it rather than the host that passed it on (#431).
	if frame.Header.Sender != "" {
		from = frame.Header.Sender
	}

	if frame.Header.Kind == wire.KindText {
		g.addMessage(chatMessage{from: from, text: string(frame.Payload), hidden: frame.Header.Hidden})

		return
	}

	if frame.Header.Kind == wire.KindAbort {
		g.receiveAbort(from, frame.Header.ID)

		return
	}

	g.receiveTransfer(from, frame)
}

// receiveTransfer feeds one frame to the assembler and keeps its row honest.
func (g *gui) receiveTransfer(from string, frame wire.Frame) {
	header := frame.Header

	g.mu.Lock()
	assembler := g.assembler
	entry := g.incoming[header.ID]
	g.mu.Unlock()

	if assembler == nil {
		g.addMessage(chatMessage{from: from, text: "A file arrived while no session was receiving"})

		return
	}

	if entry != nil && entry.abandoned {
		g.mu.Lock()
		delete(g.incoming, header.ID)
		g.mu.Unlock()

		if assembler.Abandon(header.ID) {
			g.setStatus("Receiving " + entry.note.name + " cancelled")
		}

		return
	}

	if entry == nil {
		entry = &incomingTransfer{
			note: &fileNote{id: header.ID, name: header.Name, size: header.Size, state: transferSending},
			from: from,
		}

		g.mu.Lock()
		if g.incoming == nil {
			g.incoming = make(map[string]*incomingTransfer)
		}

		g.incoming[header.ID] = entry
		g.mu.Unlock()

		g.addMessage(chatMessage{from: from, file: entry.note})
	}

	file, err := assembler.Add(frame)
	if err != nil {
		g.forgetIncoming(header.ID)
		g.updateTransfer(entry.note, transferFailed)
		g.setStatus("Receiving " + entry.note.name + " failed: " + err.Error())

		return
	}

	entry.received += int64(len(frame.Payload))

	if file != nil {
		g.forgetIncoming(header.ID)
		g.finishTransfer(entry.note, file)

		return
	}

	g.updateProgress(entry.note, entry.received)
}

// forgetIncoming stops tracking a transfer that has ended, one way or another.
func (g *gui) forgetIncoming(id string) {
	g.mu.Lock()
	delete(g.incoming, id)
	g.mu.Unlock()
}

// takeIncoming empties the receiving table and returns the rows it held.
//
// A transfer that was still arriving when its session ended is gone: the
// assembler deleted its partial file, so keeping a row that promises a file the
// user will never find would be a lie.
func (g *gui) takeIncoming() []*fileNote {
	g.mu.Lock()
	defer g.mu.Unlock()

	notes := make([]*fileNote, 0, len(g.incoming))
	for _, entry := range g.incoming {
		notes = append(notes, entry.note)
	}

	g.incoming = make(map[string]*incomingTransfer)

	return notes
}

// prepareReceive installs a fresh assembler for a new session and closes the
// one the previous session used, which drops any half-received file with it.
//
// The assembler writes into the staging folder, not the download folder: the
// save policy decides where a file ends up, and ask mode must be able to leave
// one unsaved without it landing somewhere the user did not choose.
func (g *gui) prepareReceive() {
	var assembler *wire.Assembler

	if dir, err := g.ensureStagingDir(); err == nil {
		assembler = wire.NewAssembler(dir, wire.DefaultTimeout)
	} else {
		g.setStatus("Received files cannot be saved: " + err.Error())
	}

	notes := g.takeIncoming()

	g.mu.Lock()
	old := g.assembler
	g.assembler = assembler
	g.mu.Unlock()

	if old != nil {
		old.Close()
	}

	for _, note := range notes {
		g.removeTransfer(note)
	}
}

// finishReceive forgets the assembler a session used, and any transfer that was
// still arriving when it ended.
func (g *gui) finishReceive() {
	g.mu.Lock()
	assembler := g.assembler
	g.assembler = nil
	g.mu.Unlock()

	if assembler != nil {
		assembler.Close()
	}

	for _, note := range g.takeIncoming() {
		g.removeTransfer(note)
	}
}

// fillFileRow draws a file row's picture and its Save action.
//
// A row with no path on disk is a transfer that is still in flight, or one that
// failed: neither has anything to save or to show, so both parts stay hidden
// and the label carries the state.
// showPicture draws res at the transcript's picture height.
func showPicture(pic *canvas.Image, res fyne.Resource) {
	pic.Resource = res
	pic.SetMinSize(fyne.NewSize(0, inlineImageHeight))
	pic.Refresh()
	pic.Show()
}

func (g *gui) fillFileRow(m chatMessage, pic *canvas.Image, preview *widgets.Text, icon *widget.Icon, save *widgets.CopyButton, open *widgets.CopyButton) {
	if m.file == nil {
		pic.Hide()
		preview.Hide()
		icon.Hide()
		save.Hide()
		open.Hide()

		return
	}

	path := m.file.path

	// A file this build can animate belongs to the player row, which draws its
	// own surface: the still preview would sit above it showing frame one
	// forever (#421).
	animated := path != "" && g.animatable(path)

	switch {
	case animated:
		pic.Hide()

	case path != "" && m.file.inline && g.decodable(path):
		if res := g.previewPicture(path); res != nil {
			showPicture(pic, res)
		} else {
			pic.Hide()
		}

	// A picture this device sent from a picker that gave no path: the row draws
	// the thumbnail taken while the staged copy existed (#303).
	case m.file.thumb != nil:
		showPicture(pic, fyne.NewStaticResource(m.file.name+".png", m.file.thumb))

	default:
		pic.Hide()
	}

	// Text is the other thing a row can show: bounded by bytes and lines, so a
	// long log is a row that ends in an ellipsis rather than a window with no
	// conversation under it.
	drawn := false

	if path != "" && showsText(mimeOf(m.file.name), m.file.size) {
		if body := g.previewText(path); body != "" {
			preview.SetText(body)
			preview.Show()
			drawn = true
		} else {
			preview.Hide()
		}
	} else {
		preview.Hide()
	}

	// A row that draws nothing says what kind of file it is: the icon is the
	// only thing distinguishing a PDF from a song (#304).
	if pic.Visible() || drawn || animated {
		icon.Hide()
	} else {
		icon.SetResource(fileIconFor(m.file.name))
		icon.Show()
	}

	// A file this program cannot draw is offered to the device instead: a PDF to
	// its reader, an audio clip to its player. The button appears only where it
	// can do something — the file is on this disk, the row is not already showing
	// its content, and the platform lets a file leave the app at all (#305).
	//
	// A row with no local path cannot be opened or saved from here, whatever
	// the state says: there is nothing on this disk to hand to anything. A send
	// from a phone is exactly that row (#303).
	switch {
	case path == "" || drawn || m.file.inline || !canOpenFiles():
		open.Hide()
	case m.file.state == transferSaved || m.file.state == transferSent || m.file.state == transferPending:
		// A pending file is in the app's storage, which is still a path this
		// desktop can hand to the system opener: a received PDF does not have
		// to be saved before it can be read (#469).
		open.SetOnTapped(func() { g.openFile(path) })
		open.Show()
	default:
		open.Hide()
	}

	switch m.file.state {
	case transferSent:
		save.Hide()

	case transferSending:
		// The one action slot a row has: while the bytes are moving it is the
		// way to stop them, and when they have landed it is Save as….
		if m.file.id != "" {
			save.SetText("Cancel")
			save.SetOnTapped(func() { g.cancelTransfer(m.file.id) })
			save.Show()
		} else {
			save.Hide()
		}

	case transferSaved:
		if path == "" {
			save.Hide()

			break
		}

		save.SetText("Save as…")
		save.SetOnTapped(func() { g.saveAs(path) })
		save.Show()

	case transferPending:
		if path == "" {
			save.Hide()

			break
		}

		name := filepath.Base(path)
		note := m.file
		save.SetText("Save as…")
		save.SetOnTapped(func() { g.askWhereToSave(note, name, path) })
		save.Show()

	default:
		save.Hide()
	}
}

// openFile hands a file to whatever this device uses for that kind of file.
//
// It is the answer for the kinds this program has no engine for — a PDF, an
// audio clip — and it goes through the same opener the About tab's links use,
// so there is one place that reports "no handler for this" instead of two.
func (g *gui) openFile(path string) {
	g.openPage(storage.NewFileURI(path).String())
}

// decodableImage reports whether the file is an image the toolkit can actually
// draw.
//
// The name and the MIME say what a file claims to be; a truncated download, a
// renamed text file or a type the toolkit has no decoder for draws an empty box,
// and an empty box in the transcript reads as a failed transfer. The header is
// enough to tell, and it is cheap — no pixels are decoded.
func decodableImage(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()

	_, _, err = image.DecodeConfig(file)

	return err == nil
}

// showImage opens a received picture at the size the window allows, which is
// the "tap to open it full size" half of the inline row.
//
// A non-modal popup rather than a dialog: a dialog is modal and a tap outside
// it does nothing, so the only way out was the Close button — on a phone, the
// far corner of a screen held in one hand. A popup is dismissed by a tap
// anywhere the picture is not (#387), which is what every picture viewer does.
func (g *gui) showImage(path string) {
	res, err := fyne.LoadResourceFromPath(path)
	if err != nil {
		g.showError(err)

		return
	}

	pic := canvas.NewImageFromResource(res)
	pic.FillMode = canvas.ImageFillContain

	size := g.dialogSize()
	pic.SetMinSize(fyne.NewSize(size.Width*0.8, size.Height*0.8))

	// The popup exists only so Close can reach it; the dismiss-on-tap is the
	// toolkit's, and the picture's own size is what bounds the view.
	var view *widget.PopUp

	closeBtn := widgets.NewButton("Close", func() { view.Hide() }, g.copyLabel)
	body := container.NewBorder(nil, closeBtn, nil, nil, pic)

	view = widget.NewPopUp(body, g.win.Canvas())
	view.Resize(size)
	view.Show()
}

// saveAs copies a received, already verified file wherever the user asks.
//
// The file is saved as it arrives; this action is a copy, not the save itself,
// so a failed copy leaves the verified original alone. Asking before saving is
// the policy #114 owns; this is the manual action it starts from.
func (g *gui) saveAs(path string) {
	save := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
		if err != nil {
			g.showError(err)

			return
		}

		if writer == nil {
			return
		}

		go func() {
			defer writer.Close()

			if err := copyInto(writer, path); err != nil {
				g.showError(err)

				return
			}

			g.setStatus("Saved " + writer.URI().Name())
		}()
	}, g.win)

	save.SetFileName(filepath.Base(path))
	save.Resize(g.dialogSize())

	// Start where files are meant to go, so a save is a confirmation rather
	// than a hunt through the toolkit's places (#469).
	if start := g.folderStart(g.fileSettings().folder); start != nil {
		save.SetLocation(start)
	}

	save.Show()
}

// downloadDir is where received files land by default: the user's Downloads
// folder on a desktop, the app's own storage on a phone. A folder chosen in the
// Files dialog replaces it.
func (g *gui) downloadDir() string {
	return resolveDownloadDir(g.mobile, g.storageRoot())
}

// stagingDir is where a file is assembled before it is saved.
func (g *gui) stagingDir() string {
	return defaultStagingDir(g.storageRoot())
}

// storageRoot is the app's own directory, or "" when the toolkit has not
// provided one, in which case the defaults fall back to the temporary dir.
func (g *gui) storageRoot() string {
	if g.app == nil {
		return ""
	}

	if root := g.app.Storage().RootURI(); root != nil {
		return root.Path()
	}

	return ""
}

// ensureStagingDir makes the staging folder, so the first chunk of a transfer
// does not fail on a folder that does not exist yet.
func (g *gui) ensureStagingDir() (string, error) {
	dir := g.stagingDir()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating the staging folder: %w", err)
	}

	return dir, nil
}

// ensureDownloadDir makes the download folder before a picked file is staged
// beside it, which keeps the staging copy on the same filesystem as the files.
func (g *gui) ensureDownloadDir() (string, error) {
	dir := g.downloadDir()

	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating the download folder: %w", err)
	}

	return dir, nil
}

// preferences is the toolkit's key/value store, or nil when there is no app.
func (g *gui) preferences() preferenceStore {
	if g.app == nil {
		return nil
	}

	return g.app.Preferences()
}

// fileSettings is where received files go, and whether the user is asked first.
func (g *gui) fileSettings() fileSettings {
	fallback := g.downloadDir()
	settings := readFileSettings(g.preferences(), fallback)

	// A remembered folder can stop working — it was chosen on a phone that
	// handed back something that is not a directory, or it was on a card that
	// is no longer mounted. Falling back is better than failing every file
	// against a path nobody can fix from the transcript (#286).
	if err := usableFolder(settings.folder); err != nil {
		settings.folder = fallback
	}

	return settings
}

// dialogSize is a sensible size for a picker or a picture: most of the window,
// or a fixed default while the canvas has not been measured.
func (g *gui) dialogSize() fyne.Size {
	if g.win == nil || g.win.Canvas() == nil {
		return fyne.NewSize(700, 500)
	}

	size := g.win.Canvas().Size()
	if size.Width <= 0 || size.Height <= 0 {
		return fyne.NewSize(700, 500)
	}

	return fyne.NewSize(size.Width*0.9, size.Height*0.8)
}
