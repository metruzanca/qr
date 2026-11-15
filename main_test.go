package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/color"
	"io"
	"strings"
	"testing"

	"github.com/makiuchi-d/gozxing"
	zxingqr "github.com/makiuchi-d/gozxing/qrcode"
	qrcode "github.com/skip2/go-qrcode"
)

// TestRun covers the CLI contract: arguments, stdin, multiple inputs and
// scheme handling.
func TestRun(t *testing.T) {
	t.Run("stdin", func(t *testing.T) {
		var out bytes.Buffer
		if err := run(nil, strings.NewReader("example.com\n"), &out); err != nil {
			t.Fatal(err)
		}
		if got := decodeOutput(t, out.String()); got != "https://example.com" {
			t.Errorf("decoded %q", got)
		}
	})

	t.Run("raw", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"-raw", "hello"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		if got := decodeOutput(t, out.String()); got != "hello" {
			t.Errorf("decoded %q", got)
		}
	})

	t.Run("multiple", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"a.example", "b.example"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		got := out.String()
		for _, want := range []string{"https://a.example", "https://b.example"} {
			if !strings.Contains(got, want) {
				t.Errorf("output missing header %q", want)
			}
		}
	})

	t.Run("pipe is colorless", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"example.com"}, strings.NewReader(""), &out); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out.String(), "\x1b") {
			t.Error("expected no ANSI escapes when writing to a buffer")
		}
	})

	t.Run("bad level", func(t *testing.T) {
		var out bytes.Buffer
		if err := run([]string{"-level", "nope", "x"}, strings.NewReader(""), &out); err == nil {
			t.Error("expected an error for an unknown level")
		}
	})
}

// decodeOutput reads plain-glyph output back into the encoded text.
func decodeOutput(t *testing.T, out string) string {
	t.Helper()
	grid, err := parseOutput(out, true)
	if err != nil {
		t.Fatal(err)
	}
	text, err := decodeGrid(grid)
	if err != nil {
		t.Fatalf("decoding output: %v", err)
	}
	return text
}

// TestHelp checks the help screen content and that no input is fatal for pipes.
func TestHelp(t *testing.T) {
	fs := flag.NewFlagSet("qr", flag.ContinueOnError)
	fs.Bool("plain", false, "no ANSI colors")
	var out bytes.Buffer
	writeHelp(&out, fs)
	for _, want := range []string{"Usage:", "Examples:", "qr https://example.com", "-plain"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help missing %q:\n%s", want, out.String())
		}
	}

	if err := run([]string{"-h"}, strings.NewReader(""), io.Discard); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("run(-h) = %v, want flag.ErrHelp", err)
	}

	// A pipe with no data has nothing to encode and must not print help.
	var quiet bytes.Buffer
	if err := run(nil, strings.NewReader(""), &quiet); err == nil {
		t.Error("expected an error for empty stdin")
	}
	if quiet.Len() != 0 {
		t.Errorf("expected no stdout for empty stdin, got %q", quiet.String())
	}
}

func TestNormalize(t *testing.T) {
	tests := []struct{ in, want string }{
		{"example.com", "https://example.com"},
		{"https://example.com/x?y=1", "https://example.com/x?y=1"},
		{"notion://www.notion.so/page-1234", "notion://www.notion.so/page-1234"},
		{"mailto:hi@example.com", "mailto:hi@example.com"},
	}
	for _, tt := range tests {
		if got := normalize(tt.in); got != tt.want {
			t.Errorf("normalize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// TestRenderDecodes renders a QR code, turns the terminal output back into an
// image, and checks that a real decoder reads the original text out of it. This
// covers both glyph layout and module polarity for every render mode.
func TestRenderDecodes(t *testing.T) {
	const text = "https://expo.dev/artifacts/eas/CRWtnF4Qa2UV7mHFmNLstz1Nlk8xC3bL3xQj-1Q9LNY.apk"

	for _, tt := range []struct {
		name  string
		plain bool
	}{
		{"plain", true},
		{"ansi", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, err := qrcode.New(text, qrcode.Medium)
			if err != nil {
				t.Fatal(err)
			}
			code.DisableBorder = true

			var buf bytes.Buffer
			if err := render(&buf, code.Bitmap(), 4, tt.plain, false); err != nil {
				t.Fatal(err)
			}

			grid, err := parseOutput(buf.String(), tt.plain)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeGrid(grid)
			if err != nil {
				t.Fatalf("decoding rendered output: %v", err)
			}
			if got != text {
				t.Errorf("decoded %q, want %q", got, text)
			}
		})
	}
}

func TestRenderShape(t *testing.T) {
	code, err := qrcode.New("hello", qrcode.Low)
	if err != nil {
		t.Fatal(err)
	}
	code.DisableBorder = true
	dim := len(code.Bitmap())

	var buf bytes.Buffer
	if err := render(&buf, code.Bitmap(), 2, true, false); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	if want := (dim + 4 + 1) / 2; len(lines) != want {
		t.Errorf("got %d lines, want %d", len(lines), want)
	}
	for i, line := range lines {
		if n := len([]rune(line)); n != dim+4 {
			t.Errorf("line %d: width %d runes, want %d", i, n, dim+4)
		}
	}
}

// parseOutput converts rendered terminal output into a dark/light grid, where
// grid[y][x] is true for dark modules.
func parseOutput(out string, plain bool) ([][]bool, error) {
	var grid [][]bool
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		var top, bottom []bool
		if plain {
			for _, r := range line {
				switch r {
				case '█':
					top = append(top, true)
					bottom = append(bottom, true)
				case '▀':
					top = append(top, true)
					bottom = append(bottom, false)
				case '▄':
					top = append(top, false)
					bottom = append(bottom, true)
				case ' ':
					top = append(top, false)
					bottom = append(bottom, false)
				default:
					return nil, fmt.Errorf("unexpected rune %q", r)
				}
			}
		} else {
			var fgDark, bgDark bool
			for rest := line; len(rest) > 0; {
				if rest[0] == '\x1b' {
					end := strings.IndexByte(rest, 'm')
					if end < 0 {
						return nil, fmt.Errorf("unterminated escape sequence in %q", line)
					}
					code := strings.TrimPrefix(rest[1:end], "[")
					switch code {
					case "30":
						fgDark = true
					case "97":
						fgDark = false
					case "40":
						bgDark = true
					case "107":
						bgDark = false
					case "0":
						fgDark, bgDark = false, false
					default:
						return nil, fmt.Errorf("unexpected escape %q", code)
					}
					rest = rest[end+1:]
					continue
				}
				if !strings.HasPrefix(rest, "▀") {
					return nil, fmt.Errorf("unexpected output in %q", line)
				}
				top = append(top, fgDark)
				bottom = append(bottom, bgDark)
				rest = rest[len("▀"):]
			}
		}
		grid = append(grid, top, bottom)
	}
	return grid, nil
}

func decodeGrid(grid [][]bool) (string, error) {
	const scale = 4
	h := len(grid) * scale
	w := len(grid[0]) * scale
	img := image.NewGray(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.Gray{Y: 255}
			if grid[y/scale][x/scale] {
				c.Y = 0
			}
			img.SetGray(x, y, c)
		}
	}

	bmp, err := gozxing.NewBinaryBitmapFromImage(img)
	if err != nil {
		return "", err
	}
	res, err := zxingqr.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		return "", err
	}
	return res.GetText(), nil
}
