# Video in the app

## ⚡ In a hurry

- 🎞️ **Animated GIF** — plays in its row: Play/Stop, a clock, a seek bar.
- ❌ **MP4 / H.264 / WebM** — no codec in this build; they open with the device's
  player. The plan is [#389](https://github.com/fiwon123/cthrone/issues/389).

**Status: partly implemented.** An **animated GIF** plays in its row, with
Play/Stop, a clock and a seek bar — decoded in pure Go (`image/gif`), on every
platform, no dependency (#421). What a phone records — **H.264 in MP4** — still
opens with another app, because it needs a codec this build does not carry.

The player exists and is tested; the missing piece is the **decoder**. This page
records the decision for it, on
[#389](https://github.com/fiwon123/cthrone/issues/389): **cgo FFmpeg**. It says
what that costs, where it plugs in, and what would have to be proven before it
ships.

## Why not pure Go

- There is **no production-grade pure-Go H.264/H.265 decoder**. Audio could be
  decoded in Go — WAV is arithmetic, MP3 has a pure-Go library — and video
  cannot.
- A video file is a **container plus a codec**: MP4, WebM and MKV have to be
  demuxed before anything is decoded, and that is a second piece of the same
  size.
- Ogg/Theora or MJPEG would be decodable in Go, but they are not what a phone
  records or a chat sends.

## What FFmpeg brings

| Library | Job |
|---|---|
| `libavformat` | open the container, find the video and audio streams, seek |
| `libavcodec` | decode H.264/H.265/VP8/VP9 frames |
| `libswscale` | convert the decoded frame to RGBA for the window |
| `libswresample` | convert the audio track to the player's format (48 kHz stereo signed 16-bit) |

The audio half then costs nothing new: it is the same `audioEngine` #306 built,
fed a stream instead of a decoded clip.

## The build cost, per platform

| Platform | What the runner needs | Difficulty |
|---|---|---|
| Linux (GUI archive) | `libavcodec-dev libavformat-dev libswscale-dev libswresample-dev` | apt, in the existing job |
| macOS (GUI archive) | `brew install ffmpeg` | one step, on the macOS runner |
| Windows (GUI archive, cross-built with mingw) | a **cross-built FFmpeg for mingw** — not in the runner image | the hard one: either a prebuilt archive with a checksum, or building FFmpeg in the job |
| Android (APK) | an FFmpeg built with the NDK for each ABI | a build pipeline of its own |

The headless gate stays `CGO_ENABLED=0`, so it keeps compiling the GUI tree
without FFmpeg — which is why the integration has to be behind a build tag (see
below) and why the *native link* becomes a CI-only claim, exactly as the Fyne
GUI already is.

## Licence

FFmpeg is **LGPL-2.1-or-later** when it is configured without `--enable-gpl`
(and without `--enable-nonfree`). Linking it **dynamically** keeps cthrone's
MIT licence intact; the FFmpeg libraries are shipped alongside, with their
licence text, and the release page says so.

- Do not enable GPL components (x264, x265) — that would make the whole binary
  GPL.
- A **legal review is still owed** before a release carries it. It is not a
  thing an agent can decide.
- The Android build must not static-link a GPL FFmpeg either.

## The design

The player #399 built is a clock and a set of calls — Play, Stop, Seek,
Position. Video adds a second stream to that, not a second player:

```text
                 ┌──────────────┐
  file ──demux──▶│ libavcodec   │──frame──▶ swscale ──RGBA──▶ canvas.Image
                 └──────────────┘
                 ┌──────────────┐
        audio ──▶│ swresample   │──PCM──▶ audioEngine (48 kHz, stereo, s16)
                 └──────────────┘
```

- **A decoder seam**, beside `audioEngine`: `decodeVideo` returns a `videoClip`
  (frames and delays) for a GIF today, and an FFmpeg implementation would return
  the same type for MP4. The player (`videoPlayer`) is format-blind: it shows
  frames and delays, so the binding is the only new code FFmpeg needs. The demo
  of that shape is #421's animated-GIF player.
- **Decode off the UI goroutine.** `libavcodec` is not fast enough to run in a
  frame callback; a decode goroutine hands finished `image.Image` frames to the
  window, which shows the newest and drops what it missed.
- **The clock is the audio's.** The audio is the thing that must not stutter, so
  the video follows the audio position (the same `Position()` the clock shows);
  a late frame is dropped, never waited for.
- **Seeking** is `av_seek_frame` to the nearest keyframe, then decode forward to
  the requested time — a seek is approximate until the keyframe, and the player
  must not show frames from before it. The clock is re-anchored the way the
  audio seek does it (#399).
- **The waveform** for a video is the audio track's peaks, computed with the
  same `audioPeaks` once the track is demuxed.

## The test plan

What a fake decoder can prove, headlessly, and should be written first:

- the controller's state machine: play → frames arrive → pause stops them →
  seek re-anchors the clock;
- a seek to 5 s on a 10 s clip lands within a frame of 5 s;
- a dropped frame does not stall the next one, and the last frame stays on
  screen when the clip ends;
- the row offers the player only when the decoder seam is present.

What needs the real thing:

- that FFmpeg decodes a phone's recording at all (H.264 in MP4, rotated
  metadata, variable frame rate);
- lip-sync against the audio track on a real window;
- seek latency on a long file.

None of that can be done on the machine this page was written on: no
`libav*` headers, no mingw, no NDK, no sound device and no toolkit.

## What it does not change

- The wire, the handshake, the pins: a file is a file before and after it is
  played.
- The rule that nothing leaves the app to be heard or seen — the bytes are
  already on disk.
- The "does not do" list on the release page stays true until this ships.
