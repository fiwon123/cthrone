# Windows icon resource

`make winres-icon` turns this file into `rsrc_windows_amd64.syso`, which the Go
linker folds into `cthrone_gui.exe` — the exe the user clicks. Without it that
exe carries the default Go icon in Explorer, on the desktop and in Alt-Tab,
because Windows reads the icon from the PE resource and never from a `.ico`
sitting next to the exe. No `.ico` is shipped for that reason.

The command-line exes in the same archive (`cthrone.exe`, `ct.exe`) are built
*without* the resource on purpose: they are the CLI, and `make gui-windows`
therefore builds them before `winres-icon` runs and removes the `.syso` right
after the GUI link.

The tool is pinned in the `Makefile` (`WINRES_VERSION`) and run with `go run`, so
it is a build-time tool and never a module dependency. The generated `.syso`
lives in the repository root — the package the exe is built from — and is
gitignored and deleted again by `make gui-windows`.

## Why `logo-256.png` and not `logo.ico`

go-winres rejects the repository's `assets/logo.ico` with `image: unknown format`
(it is a 453-byte 32×32 PNG-in-ICO) and `assets/logo-512.png` with
`image size too big, must fit in 256x256`. `assets/logo-256.png` is accepted.

What lands in the exe is a **single** 256×256 image, PNG-compressed inside the
resource (`RT_ICON` id 1, `RT_GROUP_ICON` named `APP` — verified by parsing the
PE of a built exe). Windows asks for 16/32/48/256 variants and scales this one
down, which is why the exe is small and why the icon stays sharp at the sizes
Windows actually uses. A multi-image group would need a multi-image `.ico` as
the input, which `cmd/assets/gen.go` does not produce.

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
