package main

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"docksmith/builder"
	"docksmith/manifest"
	"docksmith/parser"
	"docksmith/runtime"
	"docksmith/store"
)

func main() {
	args := os.Args[1:]

	if len(args) == 0 {
		printUsage()
		os.Exit(1)
	}

	// Handle the child process re-exec for namespace isolation.
	if args[0] == "__child__" {
		if err := runtime.ChildProcess(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "child process error: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	command := args[0]
	switch command {
	case "build":
		if err := runBuild(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "run":
		exitCode, err := runRun(args[1:])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Container exited with code %d\n", exitCode)
		os.Exit(exitCode)
	case "images":
		if err := runImages(); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "rmi":
		if err := runRmi(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "import":
		if err := runImport(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}
