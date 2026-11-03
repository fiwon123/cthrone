# Working with messages

## ⚡ In a hurry

| I want to… | Do this |
|---|---|
| 📋 **copy one message** | tap the row |
| 📋 **copy a part of the conversation** | **Select**, tap the first message, tap the last, **Copy** |
| 📋 **copy everything** | **Copy all** in the chat header |
| 🗑️ **delete a message** | **Select** it, then **Delete** |
| 🧹 **start clean** | **Select**, then **Clear all** |
| 🙈 **send a secret** | press the eye in the composer before typing |

Every one of those acts on **this device's transcript only**. Nothing is
recalled from the other device, and nothing is sent to it.

## Copy

A tap on a row puts the whole row on the clipboard — sender and text, the same
line the transcript shows. That is still the fastest copy for one message, and
it is the gesture a chat app teaches.

**Copy all** in the header copies the conversation in order, one message per
line. It is off while the transcript is empty.

## Select a range

Dragging Fyne's selection handles through a tall, wrapping message is hard on a
phone, so the range has a gesture of its own:

1. press **Select** in the chat header;
2. tap the first message you want;
3. tap the last one.

The rows in the span are marked with **✓** on their sender line, the header says
how many are selected, and it offers:

- **Copy** — the span, one message per line;
- **Delete** — remove it from this device;
- **Clear all** — empty the transcript, after a confirmation;
- **Cancel** — leave the selection without changing anything.

Either end can be tapped first. A single message is a range of one.

## Delete, and start clean

**Delete** removes the selected rows locally. A file row that was waiting to be
saved takes the app's own staged copy with it; a file already saved, and a file
this device sent, are never touched.

**Clear all** is the "start clean" action: it empties the transcript after
asking, cancels transfers still running, and removes the app's unsaved copies.
Saved files stay where they are.

The transcript is held in memory. Closing the window ends it, and no history is
written to disk or sent to a peer.

## Hidden messages

Press the **eye** in the composer before typing a password, a key or anything
else that should not be read over a shoulder:

- the field is masked while you type, and on a phone the platform's password
  keyboard is what opens;
- the row you send is masked too, and so is the row on the other device — the
  flag travels in the frame's header, and an older peer ignores it and shows the
  text. A terminal peer prints it as plain text, because the terminal has no
  masked row;
- **tap a masked row once to reveal it, and again to copy it**. Copy all, and a
  deliberate copy, keep the real text.

> ⚠️ **Hidden is presentation, not encryption.** Every payload is already sealed
> end to end; hidden only decides how the row is drawn. The other device — and a
> websocket host — can read the text, as they can read any message.

## Files

Received files are **not saved automatically**: the row says **not saved**, and
**Save as…** puts the file where you choose. Turn on **Save received files
automatically** in the **Files** dialog if a conversation expects a queue of
files; the row then says where each one landed.

The same dialog lists the files this conversation received and where each one
is. Tap a row to copy its path. On a desktop, **Open folder** opens the folder.

See [Send a file](Send-a-file) for the file actions themselves.

---

<sub>🔑 **Previous:** [Host and connect](Host-and-connect) · 🏠 [Home](Home) · **Next:** [Send a file](Send-a-file) 📎</sub>
