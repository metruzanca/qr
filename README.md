# qr

A tiny Go CLI that prints a scannable QR code for a URL, URI, or short piece of
text straight into your terminal.

```console
$ qr https://expo.dev/artifacts/eas/CRWtnF4Qa2UV7mHFmNLstz1Nlk8xC3bL3xQj-1Q9LNY.apk
```

Point your phone camera at the terminal and open the link.

## Install

```console
$ go install github.com/metruzanca/qr@latest
```

Or build from a checkout:

```console
$ go build -o qr .
```

## Usage

```console
qr https://example.com
qr notion://www.notion.so/My-Page-1234abcd
qr mailto:hi@example.com
qr example.com              # https:// is added automatically
echo example.com | qr       # read inputs from stdin, one per line
qr a.example b.example      # several codes, printed one after another
qr                          # no input and a terminal: shows the help screen
```

Flags:

| Flag      | Meaning                                                        |
| --------- | -------------------------------------------------------------- |
| `-level`  | error correction: `low`, `medium` (default), `quartile`, `highest` |
| `-border` | quiet zone in modules, default `4`                             |
| `-plain`  | skip ANSI colors and draw modules with block glyphs            |
| `-invert` | swap dark and light modules                                    |
| `-raw`    | encode the input exactly, without adding a `https://` scheme   |

## Rendering notes

On a terminal the code is drawn with explicit black and white ANSI colors, so
the polarity is correct whatever your color theme is. That matters: a lot of
phone scanners refuse fully inverted QR codes, which is what you get if you
rely on the terminal's own foreground and background.

When stdout is not a terminal (a pipe, a file, `less`) the CLI switches to plain
half-block glyphs with no escape codes. `-plain` forces that mode, and
`-invert` flips it if your setup needs a light-on-dark code.

## Tests

```console
$ go test ./...
```

The tests render codes in both modes, convert the terminal output back into an
image and decode it, so the glyph layout and module polarity are checked
end to end rather than just snapshotted.
