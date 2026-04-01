package utils

import (
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

func (lg *Logger) Info(msg string, args ...any) {
	lg.l.Printf("INFO: "+msg, args...)
}

func (lg *Logger) Warn(msg string, args ...any) {
	lg.l.Printf("WARN: "+msg, args...)
}

func (lg *Logger) Error(msg string, args ...any) {
	lg.l.Printf("ERROR: "+msg, args...)
}
