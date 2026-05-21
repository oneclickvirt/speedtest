package sp

import "log"

type simpleLogger struct{}

func (l *simpleLogger) Info(msg string) {
	log.Printf("[speedtest] %s", msg)
}

func (l *simpleLogger) Sync() error {
	return nil
}

var Logger = &simpleLogger{}

func InitLogger() {}