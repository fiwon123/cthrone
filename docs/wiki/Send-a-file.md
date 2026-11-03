# Send a file

## ⚡ In a hurry

In the window, press 📎 **Attach** and pick a file. The row reports the transfer
and then offers **Save as…**; a picture, a text file or an animated GIF is drawn
in the row, and a WAV/MP3 plays there with a waveform. The terminal has no file
command — text only.

Files travel the same sealed session as text, in 256 KiB chunks, and are
reassembled on the other side.

## Sending

In the GUI, **Attach** picks a file; the progress appears in the conversation as
a row of its own. The terminal has no file command — text only.

## Receiving

Nothing is written into Downloads unless you ask for it. The row reports the
transfer, then says **not saved** and offers **Save as…**: the verified file
waits in the app's own storage until you choose where it goes.

Turn on **Save received files automatically** in the **Files** dialog if you
want arrivals to land in a folder on their own; each row then says **saved to
<path>**. The same dialog lists the files this conversation received and where
each one is: tap a row to copy its path, and on a desktop **Open folder** opens
the folder.

A picture the toolkit can decode is drawn inline under the caption, so a
screenshot arrives as a picture rather than a filename. A file the app cannot
draw — a PDF, an audio clip, an archive — shows an icon for its kind beside the
caption, so two different files do not read as the same line.

**Open** hands a file to whatever the device uses for that type. It is offered on
a desktop, including for a file still waiting to be saved — the app's copy is on
the disk.

On Android, **Open** serves the file to the device's **browser** from a
one-file server on loopback: a PDF, a picture, an audio or video clip the browser
can show is shown there, and a type it cannot show is downloaded. The listener
is bound to `127.0.0.1`, the link carries a random token, and it closes after ten
minutes, on the next Open, or when the app closes. It is not the phone's own PDF
app — that needs a content provider this APK cannot carry yet
([#470](https://github.com/fiwon123/cthrone/issues/470)). If the browser shows a
blank page, keep the app open for the fetch; if it still fails, press **Save
as…** and open the file from the file manager.

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

## Playing an animated picture

An **animated GIF** plays in its row: **Play** starts it and becomes **Stop**,
the clock shows the position, and the bar under the picture moves you through
it. It is decoded when you press Play, not when the row is painted, and only one
animation is kept in memory at a time — a picture that moves is a picture that
costs.

The player is the same controller a video decoder would feed; what is missing is
the decoder, not the player (see [Video in the app](Video-in-the-app)).

## Known gaps

- **A GIF plays; a video does not.** MP4, H.264 and WebM need a codec this build
  does not carry, so those rows keep **Open** (desktop, or the browser on a
  phone). The decision and the plan are on [Video in the app](Video-in-the-app)
  ([#389](https://github.com/fiwon123/cthrone/issues/389)).
- A **PDF is not drawn in the app**: it opens in the device's reader on a
  desktop, and in the browser on a phone, by design.
- **Audio plays only as WAV or MP3.** Ogg, AAC/M4A and FLAC have no decoder in
  this build; those rows offer Open or Save as…, as before.

---

<sub>🔑 **Previous:** [Pairing and peers](Pairing-and-peers) · 🏠 [Home](Home) · **Next:** [Video in the app](Video-in-the-app) 🎞️</sub>
