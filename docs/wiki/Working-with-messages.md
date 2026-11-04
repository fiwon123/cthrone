# Working with messages

## ⚡ In a hurry

| I want to… | Do this |
|---|---|
| 📋 **copy one message** | press the copy button on its row |
| 📋 **copy a part of the conversation** | **Select**, tap the first message, tap the last, **Copy** |
| 📋 **copy everything** | **Copy all** in the chat header |
| 🗑️ **delete a message** | **Select** it, then **Delete** |
| 🧹 **start clean** | **Clear all** in the chat header |
| 🙈 **send a secret** | press the eye in the composer before typing |
| 🙈 **show or hide it again** | press the eye on its row |

Every one of those acts on **this device's transcript only**. Nothing is
recalled from the other device, and nothing is sent to it.

## Copy

Every message row carries a **copy button** at the end of its sender line. It
puts that **message text** on the clipboard — no `You:` and no `laptop:` in
front of it. The sender stays on screen, where the row draws it.

**Copy all** in the header copies the conversation in order, one message text
per line. It is off while the transcript is empty.

A tap on a row copies nothing: it is where a selection starts.

## Select a range

Dragging Fyne's selection handles through a tall, wrapping message is hard on a
phone, so the range has a gesture of its own:

1. press **Select** in the chat header;
2. tap the first message you want;
3. tap the last one.

The rows in the span are marked with **✓** on their sender line, the header says
how many are selected, and it offers:

- **Copy** — the selected messages' text, one per line;
- **Delete** — remove it from this device;
- **Cancel** — leave the selection without changing anything.

**Clear all**, which empties the whole transcript, is a button in the chat
header rather than one of these: selecting messages and emptying the conversation
are different jobs.

Either end can be tapped first. A single message is a range of one.

## Delete, and start clean

**Delete** removes the selected rows locally. A file row that was waiting to be
saved takes the app's own staged copy with it; a file already saved, and a file
this device sent, are never touched.

**Clear all** in the chat header is the "start clean" action: it empties the
transcript after asking, cancels transfers still running, and removes the app's
unsaved copies.
Saved files stay where they are.

The transcript is held in memory. Closing the window ends it, and no history is
written to disk or sent to a peer.

## Hidden messages

A message that **is hidden** — sent with the composer's eye, or received with
the flag set — carries an **eye**, and it toggles: press it to show the message
on this screen, press it again to hide it. A message that was never hidden has
no mask control, because there is nothing to uncover. A file row has no eye
either.

The eye and the row's copy button are the same square, side by side at the end of
the sender's line.

To send a message **as a password**, press the eye in the composer before
typing:

- the field is masked while you type, and on a phone the platform's password
  keyboard is what opens;
- the row you send is masked too, and so is the row on the other device — the
  flag travels in the frame's header, and an older peer ignores it and shows the
  text. A terminal peer prints it as plain text, because the terminal has no
  masked row;
- **copying a masked row copies its text**, and the row's copy button does it
  without revealing anything on screen.

> ⚠️ **Hidden is presentation, not encryption.** Every payload is already sealed
> end to end; hidden only decides how the row is drawn. The other device — and a
> websocket host — can read the text, as they can read any message.

## Files

Received files are **not saved automatically**: the row says **not saved**, and
**Save as…** puts the file where you choose. Turn on **Save received files
automatically** in the **Files** dialog if a conversation expects a queue of
files; the row then says where each one landed.

The same dialog lists the files this conversation received and where each one
is. Tap a row to copy its path. On a desktop, **Open folder** opens the folder;
a phone does not offer it, because Android and iOS will not let an app pass a
private folder to the file manager. On a phone the folder is a full-width line
with **Choose…** under it.

See [Send a file](Send-a-file) for the file actions themselves.

---

<sub>🔑 **Previous:** [Host and connect](Host-and-connect) · 🏠 [Home](Home) · **Next:** [Send a file](Send-a-file) 📎</sub>
