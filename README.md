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

Or install it with [mise](https://mise.jdx.dev):

```console
$ mise use -g go:github.com/metruzanca/qr
```

Or grab a prebuilt binary for Linux, macOS or Windows from the
[releases page](https://github.com/metruzanca/qr/releases), or build from a
checkout:

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

| Flag       | Meaning                                                          |
| ---------- | ---------------------------------------------------------------- |
| `--level`  | error correction: `low`, `medium` (default), `quartile`, `highest` |
| `--border` | quiet zone in modules, default `2`                               |
| `--plain`  | draw with block glyphs instead of ANSI colors, even on a terminal |
| `--dark`   | the background is dark: light modules become the blocks          |
| `--light`  | the background is light, the default                             |
| `--raw`    | encode the input exactly, without adding a `https://` scheme     |

Both `--flag` and `-flag` parse, but the docs use two dashes throughout.

## Rendering notes

On a terminal the code is drawn with explicit black and white ANSI colors, so
the polarity is correct whatever your color theme is. That matters: a lot of
phone scanners refuse fully inverted QR codes, which is what you get if you
rely on the terminal's own foreground and background.

When stdout is not a terminal (a pipe, a file, `less`) the CLI switches to plain
half-block glyphs with no escape codes. `--plain` forces that mode on a terminal
too, which is what you want when the output is copied somewhere else. Glyphs
use the terminal's foreground color, so pass `--dark` when that background is
dark; `--light` is the default and exists to state the intent explicitly.

```console
$ qr --plain --dark example.com    # plain text, light modules as blocks
```

## Agents

`qr --help` ends with a block you can copy straight into `AGENTS.md` or
`CLAUDE.md`:

```markdown
## Links

When a reply includes a URL longer than 40 characters, also show a QR code:

    qr --plain --dark "<url>"

Paste the output unchanged in a fenced code block with no language tag.
The glyphs are data, not ASCII art: do not re-wrap, re-indent or tidy them.
Use --light instead of --dark on a light background.
```

`--plain` matters here: it forces block glyphs even when the agent's stdout is a
terminal, so no escape codes end up in the pasted text.

## Tests

```console
$ go test ./...
```

The tests render codes in both modes, convert the terminal output back into an
image and decode it, so the glyph layout and module polarity are checked
end to end rather than just snapshotted.
