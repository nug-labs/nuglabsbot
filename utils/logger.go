package utils

import (
	"context"
	"log"
	"os"
)

type Logger struct {
	l *log.Logger
}

func NewLogger() *Logger {
	return &Logger{
		l: log.New(os.Stdout, "", log.LstdFlags|log.LUTC|log.Lshortfile),
	}
}

// NewAsyncLogger returns a Logger whose writes are flushed asynchronously so callers do not block on stdout.
// On ctx cancellation the buffer is drained, then the worker exits.
func NewAsyncLogger(ctx context.Context) *Logger {
	ch := make(chan []byte, 512)
	go func() {
		for {
			select {
			case <-ctx.Done():
				for {
					select {
					case b := <-ch:
						_, _ = os.Stdout.Write(b)
					default:
						return
					}
				}
			case b := <-ch:
				_, _ = os.Stdout.Write(b)
			}
		}
	}()
	w := &asyncLogWriter{ch: ch, ctx: ctx}
	return &Logger{l: log.New(w, "", log.LstdFlags|log.LUTC|log.Lshortfile)}
}

type asyncLogWriter struct {
	ch  chan []byte
	ctx context.Context
}

func (w *asyncLogWriter) Write(p []byte) (n int, err error) {
	p2 := make([]byte, len(p))
	copy(p2, p)
	select {
	case <-w.ctx.Done():
		_, err := os.Stdout.Write(p)
		return len(p), err
	case w.ch <- p2:
		return len(p), nil
	default:
		_, err := os.Stdout.Write(p)
		return len(p), err
	}
}

func (lg *Logger) Info(msg string, args ...any) {
	lg.l.Printf("INFO: "+msg, args...)
}

func (lg *Logger) Warn(msg string, args ...any) {
	lg.l.Printf("WARN: "+msg, args...)
}

func (lg *Logger) Error(msg string, args ...any) {
	lg.l.Printf("ERROR: "+msg, args...)
}
