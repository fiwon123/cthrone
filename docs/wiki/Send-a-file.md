# Send a file

Files travel the same sealed session as text, in 256 KiB chunks, and are
reassembled on the other side.

## Sending

In the GUI, **Attach** picks a file; the progress appears in the conversation as
a row of its own. The terminal has no file command — text only.

## Receiving

The row reports the transfer, then offers **Save as…** once it is complete. A
picture the toolkit can decode is drawn inline under the caption, so a
screenshot arrives as a picture rather than a filename. A file the app cannot
draw — a PDF, an audio clip, an archive — shows an icon for its kind beside the
caption, so two different files do not read as the same line.

**Open** hands a file to whatever the device uses for that type. It is offered on
a desktop only: Android will not let an app pass a private `file://` path to
another app, so on a phone the row offers **Save as…** instead — save it
somewhere the file manager can see, then open it from there.

## Playing an audio clip

A **WAV** or **MP3** clip the app received or sent plays on its row: **Play**
starts it, the same button becomes **Stop**, and the clock beside it shows the
position over the length. One clip sounds at a time — pressing Play on another
sends the first row back to Play — and stopping the connection silences the
device. The clip is decoded when it is played, not when the row is painted, so a
long one is decoded beside the window and starts a moment later.

The player is built into every GUI build, desktop and phone alike. It is bound to
what a chat window needs, not to a music library:

- **WAV and MP3 only.** Ogg/Vorbis, AAC/M4A and FLAC have no decoder in this
  build, so those rows keep the actions above and the icon says what they are. A
  phone's voice memo is usually AAC/M4A.
- **32 MiB of decoded sound**, a little under three minutes at the device's
  format; a longer clip is refused with a sentence rather than held in memory.
- The device is opened on the first **Play**. A machine with no audio server
  gets a status line saying the clip cannot be played, and the rest of the app
  works as before.

A multi-line message and a file both carry the sender's name: text in-band as a
prefix, a file in an optional header field that an older peer ignores.

## What does not change

- The bytes are sealed end to end like everything else; a broker or a radio
  carries ciphertext.
- No new port, no new credential, no new protocol version.
- A Bluetooth link is text in practice: a 20-byte packet turns one file chunk
  into thousands of notifications, so nothing offers a file on a radio host.

## Known gaps

- A **PDF is not drawn**: it opens in the device's reader, by design.
- **Audio plays only as WAV or MP3.** Ogg, AAC/M4A and FLAC have no decoder in
  this build; those rows offer Open or Save as…, as before.
