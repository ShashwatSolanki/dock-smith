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

func printUsage() {
	fmt.Println(`Docksmith - A simplified Docker-like build and runtime system

Usage:
  docksmith <command> [options]

Commands:
  build     Build an image from a Docksmithfile
  run       Run a container from an image
  images    List all images
  rmi       Remove an image
  import    Import a base image from a rootfs tarball
  help      Show this help message

Use "docksmith <command> --help" for more information about a command.`)
}

func runBuild(args []string) error {
	var (
		name    string
		tag     string
		noCache bool
		context string
	)

	i := 0
	for i < len(args) {
		switch args[i] {
		case "-t":
			if i+1 >= len(args) {
				return fmt.Errorf("-t requires a name:tag argument")
			}
			i++
			name, tag = parseNameTag(args[i])
		case "--no-cache":
			noCache = true
		case "--help", "-h":
			fmt.Println(`Usage: docksmith build -t <name:tag> [--no-cache] <context>

Build an image from a Docksmithfile.

Options:
  -t name:tag    Name and tag for the image (required)
  --no-cache     Skip all cache lookups and writes`)
			return nil
		default:
			if context == "" {
				context = args[i]
			} else {
				return fmt.Errorf("unexpected argument: %s", args[i])
			}
		}
		i++
	}

	if name == "" {
		return fmt.Errorf("missing -t flag: use -t name:tag")
	}
	if context == "" {
		return fmt.Errorf("missing build context directory")
	}

	absContext, err := filepath.Abs(context)
	if err != nil {
		return fmt.Errorf("resolve context path: %w", err)
	}

	instructions, err := parser.Parse(absContext)
	if err != nil {
		return fmt.Errorf("parse Docksmithfile: %w", err)
	}

	return builder.Build(instructions, builder.BuildOptions{
		Name:       name,
		Tag:        tag,
		ContextDir: absContext,
		NoCache:    noCache,
	})
}

