package main

import (
	"flag"
	"strings"
)

// parseFlags parses a flag set allowing flags and positional arguments in any
// order.
//
// The standard library stops parsing at the first non-flag argument, so
// "arena hands 83 --collection biggest" would silently ignore --collection.
// Silently is the problem: the command still succeeds, just against the wrong
// data. This reorders flags ahead of positionals before delegating.
func parseFlags(flags *flag.FlagSet, args []string) error {
	var flagArgs, positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			continue
		}

		flagArgs = append(flagArgs, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") {
			continue // value is attached
		}
		// A non-boolean flag consumes the next argument as its value.
		if found := flags.Lookup(name); found != nil && !isBoolFlag(found) && i+1 < len(args) {
			i++
			flagArgs = append(flagArgs, args[i])
		}
	}

	return flags.Parse(append(flagArgs, positional...))
}

func isBoolFlag(found *flag.Flag) bool {
	boolFlag, ok := found.Value.(interface{ IsBoolFlag() bool })
	return ok && boolFlag.IsBoolFlag()
}
