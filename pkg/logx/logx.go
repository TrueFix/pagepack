// Package logx builds Pagepack's structured logger: a compact, coloured, human-readable
// format for terminals and plain JSON or logfmt for log collectors.
package logx

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
)

// Format names accepted by LOG_FORMAT.
const (
	FormatPretty = "pretty"
	FormatJSON   = "json"
	FormatText   = "text"
)

type Options struct {
	Level  slog.Level
	Format string
	// Color forces ANSI colours on or off for the pretty format. Nil means auto-detect.
	Color  *bool
	Writer io.Writer
}

// New returns a logger for the requested format. Unknown formats fall back to pretty.
func New(o Options) *slog.Logger {
	if o.Writer == nil {
		o.Writer = os.Stderr
	}
	handlerOptions := &slog.HandlerOptions{Level: o.Level}
	switch o.Format {
	case FormatJSON:
		return slog.New(slog.NewJSONHandler(o.Writer, handlerOptions))
	case FormatText:
		return slog.New(slog.NewTextHandler(o.Writer, handlerOptions))
	}
	color := autoColor(o.Writer)
	if o.Color != nil {
		color = *o.Color
	}
	return slog.New(&prettyHandler{mu: &sync.Mutex{}, w: o.Writer, level: o.Level, color: color})
}

// ParseLevel accepts debug, info, warn/warning, and error (case-insensitive).
func ParseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "", "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	}
	return 0, fmt.Errorf("unknown log level %q", s)
}

func autoColor(w io.Writer) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("FORCE_COLOR") != "" {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

const (
	reset   = "\x1b[0m"
	bold    = "\x1b[1m"
	dim     = "\x1b[2m"
	red     = "\x1b[31m"
	green   = "\x1b[32m"
	yellow  = "\x1b[33m"
	blue    = "\x1b[34m"
	magenta = "\x1b[35m"
	cyan    = "\x1b[36m"
)

// prettyHandler prints one line per record:
//
//	14:02:07.311 INF job downloading       job=3d809ef3 downloaded=12.4 MB files=132 rate=1.1 MB/s
type prettyHandler struct {
	mu     *sync.Mutex
	w      io.Writer
	level  slog.Leveler
	color  bool
	prefix string // pre-rendered attributes from With
	group  string
}

func (h *prettyHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= h.level.Level() }

func (h *prettyHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	clone := *h
	var b strings.Builder
	b.WriteString(h.prefix)
	for _, a := range attrs {
		h.writeAttr(&b, h.group, a)
	}
	clone.prefix = b.String()
	return &clone
}

func (h *prettyHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	clone := *h
	clone.group = h.group + name + "."
	return &clone
}

func (h *prettyHandler) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	t := r.Time
	if t.IsZero() {
		t = time.Now()
	}
	b.WriteString(h.paint(dim, t.Format("15:04:05.000")))
	b.WriteByte(' ')
	b.WriteString(h.levelTag(r.Level))
	b.WriteByte(' ')
	msg := r.Message
	if pad := 22 - len(msg); pad > 0 && (r.NumAttrs() > 0 || h.prefix != "") {
		msg += strings.Repeat(" ", pad)
	}
	if r.Level >= slog.LevelError {
		b.WriteString(h.paint(bold+red, msg))
	} else {
		b.WriteString(h.paint(bold, msg))
	}
	b.WriteString(h.prefix)
	r.Attrs(func(a slog.Attr) bool { h.writeAttr(&b, h.group, a); return true })
	b.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, b.String())
	return err
}

func (h *prettyHandler) levelTag(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return h.paint(bold+red, "ERR")
	case l >= slog.LevelWarn:
		return h.paint(bold+yellow, "WRN")
	case l >= slog.LevelInfo:
		return h.paint(bold+cyan, "INF")
	default:
		return h.paint(magenta, "DBG")
	}
}

func (h *prettyHandler) writeAttr(b *strings.Builder, group string, a slog.Attr) {
	a.Value = a.Value.Resolve()
	if a.Equal(slog.Attr{}) {
		return
	}
	if a.Value.Kind() == slog.KindGroup {
		for _, inner := range a.Value.Group() {
			h.writeAttr(b, group+a.Key+".", inner)
		}
		return
	}
	key := group + a.Key
	value := formatValue(a.Value)
	b.WriteByte(' ')
	b.WriteString(h.paint(dim, key+"="))
	switch {
	case a.Key == "err" || a.Key == "error":
		b.WriteString(h.paint(red, value))
	case a.Key == "status" && a.Value.Kind() == slog.KindInt64:
		code := a.Value.Int64()
		color := green
		if code >= 500 {
			color = red
		} else if code >= 400 {
			color = yellow
		} else if code >= 300 {
			color = blue
		}
		b.WriteString(h.paint(color, value))
	case a.Key == "job":
		b.WriteString(h.paint(magenta, value))
	default:
		b.WriteString(value)
	}
}

func formatValue(v slog.Value) string {
	var s string
	switch v.Kind() {
	case slog.KindDuration:
		s = Duration(v.Duration())
	case slog.KindTime:
		s = v.Time().Format(time.RFC3339)
	case slog.KindFloat64:
		s = strconv.FormatFloat(v.Float64(), 'f', -1, 64)
	default:
		s = v.String()
	}
	// Spaces stay readable ("12.4 MB"); quote only what would be ambiguous on one line.
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return (unicode.IsSpace(r) && r != ' ') || r == '"' || r == '=' }) >= 0 {
		return strconv.Quote(s)
	}
	return s
}

// Duration renders durations at a readable precision: 850ms, 4.2s, 3m12s.
func Duration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return d.String()
	case d < time.Second:
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	case d < time.Minute:
		return strconv.FormatFloat(d.Seconds(), 'f', 1, 64) + "s"
	default:
		return d.Truncate(time.Second).String()
	}
}

func (h *prettyHandler) paint(code, s string) string {
	if !h.color {
		return s
	}
	return code + s + reset
}
