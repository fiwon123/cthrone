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

A multi-line message and a file both carry the sender's name: text in-band as a
prefix, a file in an optional header field that an older peer ignores.

## What does not change

- The bytes are sealed end to end like everything else; a broker or a radio
  carries ciphertext.
- No new port, no new credential, no new protocol version.
- A Bluetooth link is text in practice: a 20-byte packet turns one file chunk
  into thousands of notifications, so nothing offers a file on a radio host.

## Known gaps

- **Audio does not play inside the app.** The row offers the device's own player
  where the platform allows it, and on a phone the way out is Save as…; a player
  of our own needs a decoder and an audio device this toolkit does not carry
  ([#306](https://github.com/fiwon123/cthrone/issues/306)).
- A **PDF is not drawn**: it opens in the device's reader, by design.
