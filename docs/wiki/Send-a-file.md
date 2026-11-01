# Send a file

Files travel the same sealed session as text, in 256 KiB chunks, and are
reassembled on the other side.

## Sending

In the GUI, **Attach** picks a file; the progress appears in the conversation as
a row of its own. The terminal has no file command — text only.

## Receiving

The row reports the transfer, then offers **Save as…** once it is complete. A
picture the toolkit can decode is drawn inline under the caption, so a
screenshot arrives as a picture rather than a filename.

A multi-line message and a file both carry the sender's name: text in-band as a
prefix, a file in an optional header field that an older peer ignores.

## What does not change

- The bytes are sealed end to end like everything else; a broker or a radio
  carries ciphertext.
- No new port, no new credential, no new protocol version.
- A Bluetooth link is text in practice: a 20-byte packet turns one file chunk
  into thousands of notifications, so nothing offers a file on a radio host.

## Known gaps

- The sender currently sees less than the receiver: the preview the peer draws
  is not drawn on the sending side ([#237](https://github.com/fiwon123/cthrone/issues/237)).
- Text, PDF and audio do not preview inline yet — only images
  ([#237](https://github.com/fiwon123/cthrone/issues/237)).
