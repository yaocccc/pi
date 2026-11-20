package main

import "pi-goui/cmd/internal/startup"

func piArgs(args []string) ([]string, error) {
	return startup.PiArgs(args, "pi")
}
