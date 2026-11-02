# cthrone

> 💬 **Chat from one device to another** — terminal, desktop window, or phone.
> No account, no server, no cloud. One device hosts, the others connect, and
> every session is sealed end to end.

## ⚡ Start in thirty seconds

```bash
# on the device that serves
cthrone host

# on the other one, with the address the host printed
cthrone connect 192.168.1.5
```

Both screens show a **six-digit code** the first time. Compare them, accept on
the host, and type. That is the whole setup.

🖥️ Prefer a window? Run `cthrone_gui`, or `cthrone ui` from the CLI build.
📱 On a phone, install the APK and use the Setup tab.

## 🚦 Where it runs

| | Terminal | Window | Phone |
|---|:---:|:---:|:---:|
| **websocket** (default) | ✅ | ✅ | ✅ |
| **NATS** (needs a broker) | ✅ | ✅ | ✅ |
| **Bluetooth** (no network) | ✅ Linux | ✅ Linux | ❌ not yet ([#206](https://github.com/fiwon123/cthrone/issues/206)) |

## 🧭 The five pages that matter

| Page | What it answers |
|---|---|
| 🏠 [Host and connect](Host-and-connect) | the first two commands |
| 🔑 [Pairing and peers](Pairing-and-peers) | the six-digit code, and how to undo it |
| 📎 [Send a file](Send-a-file) | what the other side receives |
| 🎵 [Video in the app](Video-in-the-app) | what plays, and what needs a codec |
| 🚑 [Troubleshooting](Troubleshooting) | the failures people actually hit |

## ⚠️ What is proven, and what is not

This project says what it verified. These claims rest on hardware nobody has
run yet:

- ❌ the **Bluetooth radio on Windows** ([#224](https://github.com/fiwon123/cthrone/issues/224));
- ❌ the **phone app** on a device, and its radio ([#155](https://github.com/fiwon123/cthrone/issues/155), [#206](https://github.com/fiwon123/cthrone/issues/206));
- ❌ **video** (MP4/H.264) — the player exists, the codec does not ([#389](https://github.com/fiwon123/cthrone/issues/389));
- ✅ audio (WAV/MP3) with a waveform, a seek bar and a GIF player — tested, but not **heard** on a phone;
- ✅ everything else on the page — tested by the suite that runs on every change.

If you can run one of the ❌ rows, **[Verifying on real hardware](Verifying-on-real-hardware)**
has the steps and the issue to report on.

## 📚 Everything else

- 🧰 [Commands and flags](Commands-and-flags) · 🛰️ [NATS](NATS) · 📶 [Bluetooth](Bluetooth)
- 🌐 [Finding devices across networks](Finding-devices-across-networks)
- 🔒 [Security model](Security-model) · 🛠️ [Development](Development)

---

📦 [Releases](https://github.com/fiwon123/cthrone/releases) ·
🐛 [Issues](https://github.com/fiwon123/cthrone/issues) ·
🤝 [Contributing](https://github.com/fiwon123/cthrone/blob/dev/CONTRIBUTING.md)
