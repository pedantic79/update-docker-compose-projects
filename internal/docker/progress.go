package docker

import (
	"bytes"
	"io"
	"os"

	"github.com/docker/compose/v5/cmd/display"
	"github.com/docker/compose/v5/pkg/api"
)

const eraseToEndOfLine = "\x1b[K"

type terminalWriter interface {
	io.Writer
	IsTerminal() bool
}

func projectEventProcessor(out terminalWriter, info io.Writer) api.EventProcessor {
	if !out.IsTerminal() || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return display.Plain(out)
	}

	// Compose redraws headers without clearing shorter previous text. Clear each
	// rendered line so "pull" followed by "up" cannot leave a stale suffix.
	return display.Full(lineClearingWriter{out: out}, info, false)
}

// lineClearingWriter prevents stale characters from surviving when Compose
// redraws a terminal line with shorter content.
type lineClearingWriter struct {
	out io.Writer
}

func (w lineClearingWriter) Write(p []byte) (int, error) {
	written := 0
	for len(p) > 0 {
		newline := bytes.IndexByte(p, '\n')
		if newline < 0 {
			n, err := w.out.Write(p)
			written += n
			if err == nil && n != len(p) {
				err = io.ErrShortWrite
			}
			return written, err
		}

		if newline > 0 {
			n, err := w.out.Write(p[:newline])
			written += n
			if err != nil {
				return written, err
			}
			if n != newline {
				return written, io.ErrShortWrite
			}
		}

		n, err := io.WriteString(w.out, eraseToEndOfLine)
		if err != nil {
			return written, err
		}
		if n != len(eraseToEndOfLine) {
			return written, io.ErrShortWrite
		}

		n, err = w.out.Write(p[newline : newline+1])
		written += n
		if err != nil {
			return written, err
		}
		if n != 1 {
			return written, io.ErrShortWrite
		}
		p = p[newline+1:]
	}
	return written, nil
}
