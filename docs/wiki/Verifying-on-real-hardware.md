# Verifying on real hardware

## ⚡ In a hurry

Pick the claim you can test, follow its steps, and paste the result on its
issue — even "it worked" is the answer. Everything you need is a release build
and two devices.

Some claims in the release notes are **unproven**, because the machine that
wrote the code had none of the hardware: no phone, no Windows box, no sound
device, and no toolkit to draw a window with. Each one has an issue, and each
issue says in prose what would settle it.

This page is the missing half: the steps, the commands, and what to write down.
If you own a phone, a Windows machine, a Linux desktop with speakers, or just a
second device, you can turn one of these claims into a proven one. Reporting
"it worked" is worth as much as reporting a defect.

## Before you start

- Use a **release build**: the archive's `cthrone` / `cthrone_gui`, or the APK.
  A build from source proves something about your tree, not about the release.
- Record the **version**: `cthrone --version`, or the About tab in the window.
  Without it, a report cannot be matched to a build.
- Every test here needs **two devices**. One of them hosts; the other connects.
- Nothing is anonymous. On a websocket or a radio the two screens show a
  **six-digit first-contact code** and the host asks before the device joins:
  compare them, and photograph both screens — the comparison is the security
  step, and it is the one thing a later reader cannot reconstruct from the text.

## The Windows radio — [#224](https://github.com/fiwon123/cthrone/issues/224)

**You need:** two Windows machines, or one Windows machine and one Linux machine,
both with Bluetooth Low Energy and both running cthrone.

1. On **A**: `cthrone host --bluetooth` (or the window: Setup → transport
   *Bluetooth* → **Host**).
2. On **B**: `cthrone scan --bluetooth`. Does A appear, by name?
3. On **B**: `cthrone connect --bluetooth <name>` — or the address, in any
   spelling: `AA:BB:CC:DD:EE:FF`, `AA-BB-CC-DD-EE-FF` and `aabbccddeeff` all
   name the same device.
4. Type on both sides. Do messages cross in both directions, with the sender's
   name on the host's screen?
5. Press Ctrl-C on **B**. On **A**, does the slot free — can B (or a third
   device) join again, and does it work?
6. Repeat step 4 from the **window** on both machines, with the transport select
   set to Bluetooth in Setup.

**Write down:** the Windows edition and build, the Bluetooth adapter, whether the
scan listed A, the address string Windows' own device list shows next to the one
`cthrone scan` printed, whether the connect completed, whether text crossed both
ways, whether the host freed its slot, the six-digit code, and any error text
verbatim.

**A failure is an answer.** Open an issue per defect. Do not work around it, and
do not patch the transport in the report: the honest fallback written into #224
is to put the **refusal back** for Windows — `unsupported.go` is one build tag
away — rather than ship an option that does not work.

## The Android app — [#155](https://github.com/fiwon123/cthrone/issues/155)

**You need:** an arm64 phone, a desktop on the same network, the release APK.

1. Install the APK: tap it and allow the install once (it is debug-signed), or
   `adb install cthrone_v<version>_android_gui_arm64.apk`.
2. On the desktop: host — `cthrone host`, or the window's **Host** — and note the
   address it prints.
3. On the phone: Setup → the device name, the desktop's address, **Connect**.
4. Compare the six-digit code on both screens; accept it on the host.
5. Send text both ways. Check the sender's name, the colour of its line, and
   that the transcript scrolls.
6. Send a file from the phone and from the desktop. On both devices compare:

   ```bash
   sha256sum <file>            # Linux
   shasum -a 256 <file>        # macOS
   certutil -hashfile <file> SHA256   # Windows
   ```

7. Interrupt a transfer: press **Cancel** on the sending row, or **Stop** on the
   receiver. Is a partial file left anywhere a user would find it?
8. Turn the phone. Does the layout stay stacked (no two-column desktop layout on
   a handset)?
9. With the keyboard up: does **Enter** break the line, does **Send** send, and
   does a tap on the transcript put the keyboard away?

**Write down:** phone model and Android version, the APK version from the About
tab, the SHA-256 of each file that crossed, what worked, what did not, and
screenshots of both first-contact screens.

## The in-app player — [v0.12.0](https://github.com/fiwon123/cthrone/releases/tag/v0.12.0), tracked in [#155](https://github.com/fiwon123/cthrone/issues/155)

**You need:** a desktop with a working sound server (PulseAudio or ALSA on
Linux) and the GUI archive, plus the APK if you want to test the phone.

1. Send or receive a **WAV** or **MP3** clip. The row offers **Play**.
2. Press it: does the sound match the file, does the button become **Stop**, and
   does the clock move from `0:00` to the length?
3. Press Play on a *second* clip: does the first row go back to Play?
4. Try an **Ogg** or **M4A** file: there is no Play button, and **Open** (on a
   desktop) or **Save as…** (on a phone) is still offered.
5. A clip longer than about three minutes is refused with a sentence instead of
   being loaded.
6. Stop the sound server (`pulseaudio -k` on Linux), **start the app fresh**, then
   press Play: the status line should name the reason, and the rest of the app
   should keep working. On Linux the player asks PulseAudio first and falls back
   to ALSA, so a machine with neither is the case this step is testing.

**Write down:** format, duration, whether the sound matched, and the exact
wording of any failure.

## The window on a real screen — [#155](https://github.com/fiwon123/cthrone/issues/155)

**You need:** a desktop with the GTK3, OpenGL and X11 runtime libraries, and the
GUI archive for that platform (Linux and Windows are built; macOS from source).

1. Start `cthrone_gui`. Does a window appear, with the four tabs?
2. Resize it narrow and wide. Do the things you are meant to click stay where
   they are painted?
3. Check **both themes**: the app's own light/dark switch and the system's.
4. Open About. Is the version there, on the name's line?

**Write down:** platform, window manager, theme, and a screenshot of anything
that is clipped, overlapping, or unreadable.

## The Android radio — [#216](https://github.com/fiwon123/cthrone/issues/216), [#206](https://github.com/fiwon123/cthrone/issues/206)

**Not runnable yet.** A phone build refuses the Bluetooth transport: the
library this project uses has no Android backend, so the binding has to be
written against the Java Bluetooth APIs first. The scope decision — which role
ships first — is written on the [Bluetooth](Bluetooth) page; the spike that
follows it is #216.

What the spike must show on a device: a scan finds a peer advertising the
cthrone service, a connection opens, one packet is written and read back, and
Android's runtime permissions (`BLUETOOTH_SCAN`, `BLUETOOTH_CONNECT`) are granted
through the app rather than assumed.

## Reporting

- Put the result on the issue named above, even when it is "it worked".
- One issue per defect you find, with the version and the steps that reproduce
  it.
- Do not describe a test you did not run as passed. This project treats a
  skipped run as unverified, and so should a report.
