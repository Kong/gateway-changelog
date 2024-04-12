package cmd

import (
	"github.com/urfave/cli/v2"
)

// global flags
var (
	debug bool
)

func New() *cli.App {
	app := &cli.App{
		Name:        "changelog",
		Usage:       "changelog [command]",
		Description: "A changelog tool to manage changelog.",
		Version:     "0.0.2",

		// global flags
		Flags: []cli.Flag{
			&cli.BoolFlag{
				Name:        "debug",
				Usage:       "debug mode",
				Required:    false,
				Destination: &debug,
			},
		},

		// commands
		Commands: []*cli.Command{
			newGenerateCmd(),
		},
	}

	return app
}
