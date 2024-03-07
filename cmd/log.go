package cmd

import (
	"fmt"
	"log"
	"os"
)

func Debug(format string, v ...any) {
	if !debug {
		return
	}
	log.Printf(format, v...)
}

func Info(format string, v ...any) {
	log.Printf(format, v...)
}

func Error(format string, v ...any) {
	fmt.Fprintf(os.Stderr, format, v...)
}
