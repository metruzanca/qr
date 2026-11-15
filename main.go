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
// pass -raw to encode the text exactly as given.
package main

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
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
		level  = fs.String("level", "medium", "error correction level: low, medium, quartile, highest")
		border = fs.Int("border", 4, "quiet zone width in modules")
		plain  = fs.Bool("plain", false, "no ANSI colors; draw dark modules with block glyphs")
		invert = fs.Bool("invert", false, "swap dark and light modules")
		raw    = fs.Bool("raw", false, "encode input exactly, without adding a https:// scheme")
	)
	if err := fs.Parse(args); err != nil {
		return err
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

	// Rendered colors are only useful on a terminal; redirects and pipes get
	// plain block glyphs so the output stays readable in a file or pager.
	usePlain := *plain || !isTerminal(stdout)

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
		if err := render(stdout, code.Bitmap(), *border, usePlain, *invert); err != nil {
			return err
		}
	}
	return nil
}

// writeHelp prints the usage and examples screen. It is used both for -h and
// when the command runs without input.
func writeHelp(w io.Writer, fs *flag.FlagSet) {
	fmt.Fprint(w, `Print a QR code for a URL, URI, or short text.

Usage:
  qr [flags] <url>...
  ... | qr [flags]

Bare inputs get a https:// scheme, so "example.com" works as well as
"notion://page". Use -raw to encode the text exactly as given.

Examples:
  qr https://example.com
  qr notion://www.notion.so/My-Page-1234abcd
  qr example.com
  qr a.example b.example
  echo example.com | qr

Flags:
`)
	fs.SetOutput(w)
	fs.PrintDefaults()
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
func render(w io.Writer, bitmap [][]bool, border int, plain, invert bool) error {
	if invert {
		for y := range bitmap {
			for x := range bitmap[y] {
				bitmap[y][x] = !bitmap[y][x]
			}
		}
	}

	dim := len(bitmap)
	size := dim + 2*border

	// isDark reports whether the module at (x, y) is dark, treating the quiet
	// zone and any padding below the symbol as light.
	isDark := func(x, y int) bool {
		x -= border
		y -= border
		if x < 0 || y < 0 || x >= dim || y >= dim {
			return false
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
