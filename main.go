// Command qr prints a scannable QR code for a URL, URI, or any short text
// directly in the terminal.
//
// Usage:
//
//	qr https://example.com
//	qr notion://www.notion.so/my-page-1234
//	echo example.com | qr
//
// A scheme is added to bare inputs (example.com becomes https://example.com);
// pass --raw to encode the text exactly as given.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"reflect"
	"runtime/debug"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// ANSI escape sequences used for the colored renderer. Using explicit black
// and white keeps the code in the correct polarity regardless of the terminal
// theme, which matters because many scanners reject inverted QR codes.
const (
	escReset = "\x1b[0m"
	fgDark   = "\x1b[30m"
	fgLight  = "\x1b[97m"
	bgDark   = "\x1b[40m"
	bgLight  = "\x1b[107m"
)

// version is the release version, injected at build time by GoReleaser with
// -ldflags "-X main.version=...". Builds that skip the linker flag fall back to
// the module version recorded by `go install ...@vX.Y.Z`, then to "dev".
var version = ""

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "qr:", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("qr", flag.ContinueOnError)
	fs.SetOutput(stdout)
	fs.Usage = func() { writeHelp(fs.Output(), fs) }

	var (
		level       = fs.String("level", "medium", "error correction level: low, medium, quartile, highest")
		border      = fs.Int("border", 2, "quiet zone width in modules")
		plain       = fs.Bool("plain", false, "draw with block glyphs instead of ANSI colors, even on a terminal")
		dark        = fs.Bool("dark", false, "the background is dark: light modules are drawn with blocks (plain output only)")
		light       = fs.Bool("light", false, "the background is light, the default (plain output only)")
		raw         = fs.Bool("raw", false, "encode input exactly, without adding a https:// scheme")
		showVersion = fs.Bool("version", false, "print the version and exit")
	)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *showVersion {
		fmt.Fprintf(stdout, "qr %s\n", versionText())
		return nil
	}
	if *dark && *light {
		return errors.New("--dark and --light are mutually exclusive")
	}

	inputs := fs.Args()
	if len(inputs) == 0 {
		// Nothing to read from an interactive terminal, so show the help
		// screen instead of waiting for input.
		if isTerminal(stdin) {
			writeHelp(stdout, fs)
			return nil
		}
		lines, err := readLines(stdin)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return errors.New("no input on stdin")
		}
		inputs = lines
	}

	qlevel, err := parseLevel(*level)
	if err != nil {
		return err
	}
	if *border < 0 {
		return fmt.Errorf("border must not be negative, got %d", *border)
	}

	usePlain := usePlainOutput(*plain, isTerminal(stdout))
	darkBG := *dark

	printed := 0
	for _, in := range inputs {
		text := strings.TrimSpace(in)
		if text == "" {
			continue
		}
		if !*raw {
			text = normalize(text)
		}

		code, err := qrcode.New(text, qlevel)
		if err != nil {
			return fmt.Errorf("%q: %w", text, err)
		}
		code.DisableBorder = true

		if printed > 0 {
			fmt.Fprintln(stdout)
		}
		printed++
		if len(inputs) > 1 {
			fmt.Fprintf(stdout, "%s\n", text)
		}
		if err := render(stdout, code.Bitmap(), *border, usePlain, darkBG); err != nil {
			return err
		}
	}
	return nil
}

// versionText is the value reported by --version. The linker flag wins, so
// release builds name their tag; otherwise a `go install` binary can report the
// module version the toolchain stamped into it.
func versionText() string {
	if version != "" {
		return version
	}
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}

// writeHelp prints the usage and examples screen. It is used both for --help
// and when the command runs without input.
func writeHelp(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, `Print a QR code for a URL, URI, or short text.

Usage:
  qr [flags] <url>...
  ... | qr [flags]

Bare inputs get a https:// scheme, so "example.com" works as well as
"notion://page". Use --raw to encode the text exactly as given.

When the output is piped (a file, a pager, an agent) the code is drawn with
plain block glyphs instead of ANSI colors. --plain forces that on a terminal
too. Those glyphs use the terminal foreground color, so pass --dark when the
background is dark.

Examples:
  qr https://example.com
  qr notion://www.notion.so/My-Page-1234abcd
  qr example.com
  qr a.example b.example
  echo example.com | qr
  qr --dark example.com           # dark background, e.g. a dark terminal
  qr --plain --dark example.com   # plain text, to paste into a chat

Flags:
`)
	fs.SetOutput(w)
	writeFlags(w, fs)

	fmt.Fprint(w, `
Agents:
  Copy the lines between the fences below into AGENTS.md or CLAUDE.md, so an
  agent prints a QR code for the links it shows you:

`)
	fmt.Fprintf(w, "%smarkdown\n%s\n", mdFence, agentSnippet)
	fmt.Fprint(w, mdFence+"\n")
}

// mdFence delimits the copy-paste block in the help screen. The help text is a
// raw string literal, so backticks cannot be written inline.
const mdFence = "```"

// agentSnippet is the AGENTS.md/CLAUDE.md rule advertised by the help screen.
// Keep the lines short: it gets pasted into files that are read as plain text.
const agentSnippet = `## Links

When a reply includes a URL longer than 40 characters, also show a QR code:

    qr --plain --dark "<url>"

Paste the output unchanged in a fenced code block with no language tag.
The glyphs are data, not ASCII art: do not re-wrap, re-indent or tidy them.
Use --light instead of --dark on a light background.`

// flagOrder lists the flags in the order the help screen shows them: output
// format first, then code shape, then input handling, then the version flag.
var flagOrder = []string{"plain", "dark", "light", "level", "border", "raw", "version"}

// writeFlags prints the flag list with the double-dash prefix used in the
// examples. The flag package accepts both forms but prints single dashes, so
// the list is built by hand instead of with PrintDefaults.
func writeFlags(w io.Writer, fs *flag.FlagSet) {
	printed := make(map[string]bool, len(flagOrder))
	for _, name := range flagOrder {
		if f := fs.Lookup(name); f != nil {
			writeFlag(w, f)
			printed[name] = true
		}
	}
	// Anything new that is not in the order list above still shows up.
	fs.VisitAll(func(f *flag.Flag) {
		if !printed[f.Name] {
			writeFlag(w, f)
		}
	})
}

func writeFlag(w io.Writer, f *flag.Flag) {
	name, usage := flag.UnquoteUsage(f)
	var b strings.Builder
	b.WriteString("  --")
	b.WriteString(f.Name)
	if name != "" {
		b.WriteString(" ")
		b.WriteString(name)
	}
	b.WriteString("\n    \t")
	b.WriteString(strings.ReplaceAll(usage, "\n", "\n    \t"))
	if !isZeroValue(f) {
		if isStringFlag(f) {
			fmt.Fprintf(&b, " (default %q)", f.DefValue)
		} else {
			fmt.Fprintf(&b, " (default %v)", f.DefValue)
		}
	}
	fmt.Fprint(w, b.String(), "\n")
}

// isZeroValue reports whether the flag's default is the zero value for its
// type, in which case it is left out of the help text, matching the flag
// package's own behaviour.
func isZeroValue(f *flag.Flag) bool {
	t := reflect.TypeOf(f.Value)
	var zero reflect.Value
	if t.Kind() == reflect.Pointer {
		zero = reflect.New(t.Elem())
	} else {
		zero = reflect.Zero(t)
	}
	return f.DefValue == zero.Interface().(flag.Value).String()
}

func isStringFlag(f *flag.Flag) bool {
	t := reflect.TypeOf(f.Value)
	return t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.String
}

// usePlainOutput reports whether the code should be drawn with block glyphs
// instead of ANSI colors. Colors only help on a terminal, and --plain asks for
// text that stays intact when it is copied, so either one selects glyphs.
func usePlainOutput(plain, terminal bool) bool {
	return plain || !terminal
}

// normalize adds a https:// scheme to inputs that do not already carry one, so
// "example.com" works while "notion://page" and "mailto:a@b.c" stay untouched.
func normalize(s string) string {
	if u, err := url.Parse(s); err == nil && u.Scheme != "" {
		return s
	}
	return "https://" + s
}

func parseLevel(s string) (qrcode.RecoveryLevel, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low", "l":
		return qrcode.Low, nil
	case "medium", "m", "":
		return qrcode.Medium, nil
	case "quartile", "q", "high":
		// skip2 calls level Q "High".
		return qrcode.High, nil
	case "highest", "h":
		return qrcode.Highest, nil
	default:
		return 0, fmt.Errorf("unknown error correction level %q (want low, medium, quartile or highest)", s)
	}
}

// isTerminal reports whether v is a terminal, either for input or output.
func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func readLines(r io.Reader) ([]string, error) {
	var lines []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		if line := strings.TrimSpace(sc.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	return lines, sc.Err()
}

// render draws the QR bitmap using half-block glyphs, so two module rows are
// packed into one terminal line. Each cell sets the foreground for the top
// module and the background for the bottom one.
func render(w io.Writer, bitmap [][]bool, border int, plain, darkBG bool) error {
	// A dark background is only meaningful for glyph output. The colored
	// renderer picks explicit black and white, so its polarity is already
	// correct on any terminal and flipping it would only produce a code that
	// does not scan.
	darkBG = darkBG && plain
	if darkBG {
		for y := range bitmap {
			for x := range bitmap[y] {
				bitmap[y][x] = !bitmap[y][x]
			}
		}
	}

	dim := len(bitmap)
	size := dim + 2*border

	// isDark reports whether the module at (x, y) is dark. The quiet zone
	// follows the page background: light normally, dark when the code is drawn
	// for a dark background, so the glyph polarity stays consistent with the
	// modules around it.
	isDark := func(x, y int) bool {
		x -= border
		y -= border
		if x < 0 || y < 0 || x >= dim || y >= dim {
			return darkBG
		}
		return bitmap[y][x]
	}

	bw := bufio.NewWriter(w)
	defer bw.Flush()

	for y := 0; y < size; y += 2 {
		if !plain {
			bw.WriteString(escReset)
		}
		var lastFG, lastBG string
		for x := 0; x < size; x++ {
			top, bottom := isDark(x, y), isDark(x, y+1)
			if plain {
				// One glyph per two module rows, dark modules drawn with the
				// terminal foreground color.
				switch {
				case top && bottom:
					bw.WriteString("█")
				case top:
					bw.WriteString("▀")
				case bottom:
					bw.WriteString("▄")
				default:
					bw.WriteString(" ")
				}
				continue
			}

			fg, bg := fgLight, bgLight
			if top {
				fg = fgDark
			}
			if bottom {
				bg = bgDark
			}
			if fg != lastFG {
				bw.WriteString(fg)
				lastFG = fg
			}
			if bg != lastBG {
				bw.WriteString(bg)
				lastBG = bg
			}
			bw.WriteString("▀")
		}
		if !plain {
			bw.WriteString(escReset)
		}
		bw.WriteString("\n")
	}
	return nil
}
