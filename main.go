package main

import (
	"github.com/Kong/changelog/cmd"
	"os"
)

func main() {
	app := cmd.New()
	if err := app.Run(os.Args); err != nil {
		cmd.Error("Error: %s\n", err.Error())
		os.Exit(1)
	}
}
