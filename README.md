## CThrone

&nbsp;&nbsp;&nbsp;&nbsp; A tool to send message from a device to another one.
It simplifies process to use only commands beside any other GUI program.

Latest Releases: [Link](https://github.com/fiwon123/cthrone/releases)

## OS Support

- Windows
- Linux 
- Android

## About

For now cthrone handle only websocket and nats.

Websocket is bidirectional communication, host and connect devices can send and receive messages

Nats is one directional communication, host receive message and connect send message

## Usage
### Cthrone use as default websocket connection:

- `cthrone host` and after ctrhone `chtrone connect "IP"`

- if you don't know ip you can try to run `cthrone scan` to print all available connection

### For nats you need a local nats server to make it works, if you download the project you can just use docker to run a local nats server:

- `docker compose up`
- after that just type `cthrone host --nats` and after ctrhone `chtrone --nats`

### On Android device you need a terminal app to execute command as this is not a apk. A recommendation is use termux app.

- `pkg update && pkg upgrade`
- `pkg install golang`
- `termux-setup-storage`
- `cp /sdcard/path/ct . && cp /sdcard/path/cthrone .`
- `chmod +x ./ct && chmod +x ./cthrone` 
- `./ct` or `./cthrone`

## For more information

- `cthrone --help`
