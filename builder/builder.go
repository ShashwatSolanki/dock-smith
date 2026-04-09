package builder

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"docksmith/cache"
	"docksmith/layer"
	"docksmith/manifest"
	"docksmith/parser"
	"docksmith/runtime"
	"docksmith/store"
)

// BuildOptions holds options for the build.
type BuildOptions struct {
	Name       string
	Tag        string
	ContextDir string
	NoCache    bool
}

// Build executes the full build sequence for a Docksmithfile.
func Build(instructions []parser.Instruction, opts BuildOptions) error {
	if err := store.EnsureDirs(); err != nil {
		return fmt.Errorf("ensure dirs: %w", err)
	}

	buildStart := time.Now()

	var (
		layers      []manifest.Layer
		workdir     string
		envState    = make(map[string]string)
		cmdArgs     []string
		prevDigest  string
		cascadeMiss = opts.NoCache
		allCacheHit = true
		existingCreated string
	)

	// Try to load existing manifest to preserve created timestamp.
	existing, err := store.LoadManifest(opts.Name, opts.Tag)
	if err == nil {
		existingCreated = existing.Created
	}

	totalSteps := len(instructions)

	for i, instr := range instructions {
		stepNum := i + 1

		switch instr.Type {
		case parser.InstrFROM:
			baseManifest, err := store.LoadManifest(instr.FromImage, instr.FromTag)
			if err != nil {
				return fmt.Errorf("step %d/%d: FROM %s:%s: %w", stepNum, totalSteps, instr.FromImage, instr.FromTag, err)
			}

			layers = append(layers, baseManifest.Layers...)

			for _, e := range baseManifest.Config.Env {
				parts := strings.SplitN(e, "=", 2)
				if len(parts) == 2 {
					envState[parts[0]] = parts[1]
				}
			}
