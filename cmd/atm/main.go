package main

import (
	"os"

	"atm/internal/cli"
)

func main() {
	os.Exit(cli.Execute())
}
