//go:build gui

package ui

import (
	"context"
	"errors"
	"fmt"
	"mime"
	"os"
	"path/filepath"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
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

		g.queuePick(reader)
	}, g.win)
	picker.Resize(g.dialogSize())
	picker.Show()
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

	go func() {
		defer reader.Close()

		dir, err := g.ensureDownloadDir()
		if err != nil {
			g.setStatus("Attach failed: " + err.Error())

			return
		}

		path, size, err := stage(reader, dir)
		if err != nil {
			g.setStatus("Attach failed: " + err.Error())

			return
		}

		note := &fileNote{name: name, size: size, state: transferSending}
		item := outgoing{
			id:   newTransferID(),
			name: name,
			mime: mimeOf(name),
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

	g.mu.Lock()
	sess := g.sess
	g.mu.Unlock()

	if sess == nil {
		g.updateTransfer(item.note, transferFailed)
		g.setStatus("Nothing to send to — connect first")

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

	err = sendFile(ctx, sess, item.id, item.name, item.mime, item.size, file, func(sent int64) {
		g.updateProgress(item.note, sent)
	})

	g.untrackTransfer(item.id)

	switch {
	case err == nil:
		g.updateTransfer(item.note, transferSent)

	case errors.Is(err, context.Canceled):
		g.removeTransfer(item.note)
		g.setStatus("Sending " + item.name + " cancelled")

	default:
		g.updateTransfer(item.note, transferFailed)
		g.setStatus("Sending " + item.name + " failed: " + err.Error())
	}
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
		var found bool

		g.messages, found = dropFile(g.messages, note)
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
// The row is updated from the staged copy first, so a picture can be drawn while
// ask mode waits for an answer: the verified bytes are all there, only their
// final home is undecided.
func (g *gui) finishTransfer(note *fileNote, file *wire.File) {
	settings := g.fileSettings()

	state := transferSaved
	if settings.policy == saveAsk {
		state = transferPending
	}

	fyne.Do(func() {
		note.name = file.Name
		note.path = file.Path
		note.sent = file.Size
		note.inline = showsInline(file.MIME, file.Size)
		note.state = state
		g.refreshTransfer(note)
	})

	if settings.policy == saveAsk {
		g.askWhereToSave(note, file.Name, file.Path)

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
		fyne.Do(func() {
			note.path = ""
			note.sent = 0
			note.inline = false
			note.state = transferFailed
			g.refreshTransfer(note)
		})
		g.setStatus("Saving " + file.Name + " failed: " + err.Error())

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

// showFiles is the Files dialog: where received files go, and whether the user
// wants to be asked first.
func (g *gui) showFiles() {
	settings := g.fileSettings()

	folder := widget.NewEntry()
	folder.SetText(settings.folder)

	ask := widgets.NewCheckRow("Ask before saving each file", nil, !g.mobile)
	ask.SetChecked(settings.policy == saveAsk)

	choose := widget.NewButtonWithIcon("Choose…", theme.FolderOpenIcon(), func() {
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
	})

	items := []*widget.FormItem{
		// The picker is the normal way in on a phone, where typing a path is
		// close to impossible; the field stays for a path pasted from a desktop.
		widgets.FormRow("Folder", container.NewBorder(nil, nil, nil, choose, folder), !g.mobile),
		{Text: "", Widget: ask},
	}

	form := dialog.NewForm("Files", "Save", "Cancel", items, func(ok bool) {
		if !ok {
			return
		}

		g.storeFileSettings(folder.Text, ask.Checked())
	}, g.win)
	form.Resize(g.dialogSize())
	form.Show()
}

// storeFileSettings validates and remembers the choice, so it survives a
// restart. A folder that is not a path is refused rather than silently replaced
// by the fallback, because the user meant to change it.
func (g *gui) storeFileSettings(folder string, ask bool) {
	dir := expandFolder(folder)
	if dir == "" {
		g.setStatus("Not saved: " + folder + " is not an absolute path")

		return
	}

	settings := fileSettings{folder: dir, policy: saveAuto}
	if ask {
		settings.policy = saveAsk
	}

	writeFileSettings(g.preferences(), settings)

	if ask {
		g.setStatus("Files will be saved in " + dir + " — asking first")

		return
	}

	g.setStatus("Files will be saved in " + dir)
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

	if frame.Header.Kind == wire.KindText {
		g.addMessage(chatMessage{from: from, text: string(frame.Payload)})

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

	if entry == nil {
		entry = &incomingTransfer{
			note: &fileNote{name: header.Name, size: header.Size, state: transferSending},
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
func (g *gui) fillFileRow(m chatMessage, pic *canvas.Image, save *widget.Button) {
	if m.file == nil || m.file.path == "" {
		pic.Hide()
		save.Hide()

		return
	}

	path := m.file.path

	if m.file.inline {
		if res, err := fyne.LoadResourceFromPath(path); err == nil {
			pic.Resource = res
			pic.SetMinSize(fyne.NewSize(0, inlineImageHeight))
			pic.Refresh()
			pic.Show()
		} else {
			pic.Hide()
		}
	} else {
		pic.Hide()
	}

	switch m.file.state {
	case transferSaved:
		save.OnTapped = func() { g.saveAs(path) }
		save.Show()

	case transferPending:
		name := filepath.Base(path)
		note := m.file
		save.OnTapped = func() { g.askWhereToSave(note, name, path) }
		save.Show()

	default:
		save.Hide()
	}
}

// showImage opens a received picture at the size the window allows, which is
// the "tap to open it full size" half of the inline row.
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

	view := dialog.NewCustom(filepath.Base(path), "Close", container.NewStack(pic), g.win)
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
	return readFileSettings(g.preferences(), g.downloadDir())
}

// mimeOf names a picked file's type from its extension. The receiver uses it to
// decide between an inline picture and a file row, so an unknown type is a file
// row rather than a guess.
func mimeOf(name string) string {
	if kind := mime.TypeByExtension(filepath.Ext(name)); kind != "" {
		return kind
	}

	return "application/octet-stream"
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
