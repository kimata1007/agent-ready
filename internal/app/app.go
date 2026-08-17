package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
)

type Version struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

type Application struct {
	Input   io.Reader
	Output  io.Writer
	Errors  io.Writer
	Version Version
}

func New(input io.Reader, output, errorsWriter io.Writer) *Application {
	return &Application{
		Input:   input,
		Output:  output,
		Errors:  errorsWriter,
		Version: Version{Version: "dev", Commit: "none", Date: "unknown"},
	}
}

func (a *Application) Run(_ context.Context, arguments []string) int {
	if len(arguments) == 0 {
		a.printUsage(a.Errors)
		return 2
	}

	switch arguments[0] {
	case "help", "-help", "--help", "-h":
		a.printUsage(a.Output)
		return 0
	case "version":
		if len(arguments) != 1 {
			fmt.Fprintln(a.Errors, "Error: version accepts no arguments")
			return 1
		}
		if err := json.NewEncoder(a.Output).Encode(a.Version); err != nil {
			fmt.Fprintf(a.Errors, "Error: write version: %s\n", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(a.Errors, "Error: unknown command %q\n", arguments[0])
		return 1
	}
}

func (a *Application) printUsage(writer io.Writer) {
	fmt.Fprintln(writer, `agent-ready keeps project knowledge in ~/.agent-ready/<project>/.

Usage:
  agent-ready init [options] <project> <source...>
  agent-ready add [options] <project> <source...>
  agent-ready refresh [options] <project>
  agent-ready version`)
}
