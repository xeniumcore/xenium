package adapters

import "fmt"

type StdLogger struct{}

func (StdLogger) Infof(format string, args ...any) {
	fmt.Printf("[INFO] "+format+"\n", args...)
}

func (StdLogger) Warnf(format string, args ...any) {
	fmt.Printf("[WARN] "+format+"\n", args...)
}

func (StdLogger) Errorf(format string, args ...any) {
	fmt.Printf("[ERROR] "+format+"\n", args...)
}

func (StdLogger) Criticalf(format string, args ...any) {
	fmt.Printf("[CRITICAL] "+format+"\n", args...)
}
