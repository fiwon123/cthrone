# Windows icon resource

`make winres-icon` turns this file into `rsrc_windows_amd64.syso`, which the Go
linker folds into `cthrone_gui.exe` and `cthrone.exe`. Without it the exes carry
the default Go icon in Explorer, on the desktop and in Alt-Tab, because Windows
reads the icon from the PE resource and never from a `.ico` sitting next to the
exe.

The tool is pinned in the `Makefile` (`WINRES_VERSION`) and run with `go run`, so
it is a build-time tool and never a module dependency. The generated `.syso`
lives in the repository root — the package both exes are built from — and is
gitignored and deleted again by `make gui-windows`.

## Why `logo-256.png` and not `logo.ico`

go-winres rejects the repository's `assets/logo.ico` with `image: unknown format`
(it is a 453-byte 32×32 PNG-in-ICO) and `assets/logo-512.png` with
`image size too big, must fit in 256x256`. `assets/logo-256.png` is accepted and
is resized into the 256/64/48/32/16 variants Windows asks for.

The path is written as `../assets/logo-256.png` because go-winres resolves it
against the input directory (`winres/`) and cleans the result relative to the
working directory, which must be the repository root.

## Seeing the result

The icon is a Windows-only artifact and cannot be checked on Linux:

```sh
dumpbin /headers cthrone_gui.exe | grep .rsrc      # Developer Prompt
x86_64-w64-mingw32-objdump -h cthrone_gui.exe      # cross-check with mingw
```

`release.yml` runs the second form after `make gui-windows`, so a build that
lost the resource fails the job instead of shipping.
