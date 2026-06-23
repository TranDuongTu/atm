package main

import (
	"fmt"
	"os"
)

const version = "0.1.0"

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println("atm", version)
		os.Exit(0)
	}
	fmt.Fprintln(os.Stderr, "atm", version, "- agent tasks management")
	fmt.Fprintln(os.Stderr, "usage: atm <command> [flags]")
	fmt.Fprintln(os.Stderr, "run 'atm --help' once the CLI is wired.")
	os.Exit(0)
}