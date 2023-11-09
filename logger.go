package main

import (
	"fmt"
	"log"
	"os"
)

var (
	_ Logger = (*ConsoleLogger)(nil)
)

type Logger interface {
	Debugf(format string, args ...any)
	Infof(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

type ConsoleLogger struct {
	std   *log.Logger
	debug bool
}

func NewConsoleLogger(debug bool) Logger {
	return &ConsoleLogger{std: log.New(os.Stdout, "", log.LstdFlags), debug: debug}
}

func (c *ConsoleLogger) Debugf(format string, args ...any) {
	if c.debug {
		_ = c.std.Output(2, fmt.Sprintf("[DEBUG] "+format, args...))
	}
}

func (c *ConsoleLogger) Infof(format string, args ...any) {
	_ = c.std.Output(2, fmt.Sprintf("[INFO] "+format, args...))
}

func (c *ConsoleLogger) Warnf(format string, args ...any) {
	_ = c.std.Output(2, fmt.Sprintf("[WARN] "+format, args...))
}

func (c *ConsoleLogger) Errorf(format string, args ...any) {
	_ = c.std.Output(2, fmt.Sprintf("[ERROR] "+format, args...))
}

func (c *ConsoleLogger) Fatalf(format string, args ...any) {
	_ = c.std.Output(2, fmt.Sprintf("[FATAL] "+format, args...))
	os.Exit(1)
}
