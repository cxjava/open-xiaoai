package logger

import (
	"fmt"
	"os"
)

var debug bool

func init() {
	debug = os.Getenv("MI_DEBUG") != ""
}

func Debug(format string, args ...interface{}) {
	if debug {
		fmt.Printf("[DEBUG] "+format+"\n", args...)
	}
}

func Info(format string, args ...interface{}) {
	fmt.Printf("[OTA] "+format+"\n", args...)
}
