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
			if baseManifest.Config.WorkingDir != "" {
				workdir = baseManifest.Config.WorkingDir
			}
			if len(baseManifest.Config.Cmd) > 0 {
				cmdArgs = baseManifest.Config.Cmd
			}

			prevDigest = baseManifest.Digest

			fmt.Printf("Step %d/%d : FROM %s:%s\n", stepNum, totalSteps, instr.FromImage, instr.FromTag)

		case parser.InstrWORKDIR:
			workdir = instr.Args
			fmt.Printf("Step %d/%d : WORKDIR %s\n", stepNum, totalSteps, instr.Args)

		case parser.InstrENV:
			envState[instr.EnvKey] = instr.EnvValue
			fmt.Printf("Step %d/%d : ENV %s=%s\n", stepNum, totalSteps, instr.EnvKey, instr.EnvValue)

		case parser.InstrCMD:
			cmdArgs = instr.CmdArgs
			fmt.Printf("Step %d/%d : CMD %s\n", stepNum, totalSteps, instr.Args)

		case parser.InstrCOPY:
			stepStart := time.Now()

			fileHashes, err := layer.GetSourceFileHashes(opts.ContextDir, instr.CopySrc)
			if err != nil {
				return fmt.Errorf("step %d/%d: COPY hash source files: %w", stepNum, totalSteps, err)
			}

			cacheKey := cache.ComputeCacheKey(prevDigest, instr.FullText, workdir, envState, fileHashes)

			var layerDigest string
			var cacheHit bool

			if !cascadeMiss {
				if digest := cache.Lookup(cacheKey); digest != "" {
					layerDigest = digest
					cacheHit = true
				}
			}

			if !cacheHit {
				cascadeMiss = true
				allCacheHit = false

				tarBytes, digest, err := layer.CreateCopyLayer(opts.ContextDir, instr.CopySrc, instr.CopyDst, workdir)
				if err != nil {
					return fmt.Errorf("step %d/%d: COPY: %w", stepNum, totalSteps, err)
				}

				if err := layer.StoreTar(tarBytes, digest); err != nil {
					return fmt.Errorf("step %d/%d: store layer: %w", stepNum, totalSteps, err)
				}

				layerDigest = digest

				if !opts.NoCache {
					cache.Store(cacheKey, digest)
				}
			}

			layerSize := int64(0)
			if fi, err := os.Stat(store.LayerPath(layerDigest)); err == nil {
				layerSize = fi.Size()
			}

			layers = append(layers, manifest.Layer{
				Digest:    layerDigest,
				Size:      layerSize,
				CreatedBy: instr.FullText,
			})
			prevDigest = layerDigest

			elapsed := time.Since(stepStart)
			if cacheHit {
				fmt.Printf("Step %d/%d : %s [CACHE HIT] %.2fs\n", stepNum, totalSteps, instr.FullText, elapsed.Seconds())
			} else {
				fmt.Printf("Step %d/%d : %s [CACHE MISS] %.2fs\n", stepNum, totalSteps, instr.FullText, elapsed.Seconds())
			}

		case parser.InstrRUN:
			stepStart := time.Now()

			cacheKey := cache.ComputeCacheKey(prevDigest, instr.FullText, workdir, envState, nil)

			var layerDigest string
			var cacheHit bool

			if !cascadeMiss {
				if digest := cache.Lookup(cacheKey); digest != "" {
					layerDigest = digest
					cacheHit = true
				}
			}

			if !cacheHit {
				cascadeMiss = true
				allCacheHit = false

				tmpRoot, err := os.MkdirTemp("", "docksmith-build-*")
				if err != nil {
					return fmt.Errorf("step %d/%d: create temp dir: %w", stepNum, totalSteps, err)
				}

				digests := make([]string, len(layers))
				for j, l := range layers {
					digests[j] = l.Digest
				}
				if err := layer.ExtractLayers(tmpRoot, digests); err != nil {
					os.RemoveAll(tmpRoot)
					return fmt.Errorf("step %d/%d: extract layers: %w", stepNum, totalSteps, err)
				}

				if workdir != "" {
					os.MkdirAll(filepath.Join(tmpRoot, workdir), 0755)
				}

				beforeSnapshot, err := layer.SnapshotDir(tmpRoot)
				if err != nil {
					os.RemoveAll(tmpRoot)
					return fmt.Errorf("step %d/%d: snapshot before RUN: %w", stepNum, totalSteps, err)
				}

				var envVars []string
				for k, v := range envState {
					envVars = append(envVars, k+"="+v)
				}
				sort.Strings(envVars)
				if _, ok := envState["PATH"]; !ok {
					envVars = append(envVars, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
				}

				exitCode, err := runtime.RunIsolated(tmpRoot, []string{instr.Args}, workdir, envVars, true)
				if err != nil {
					os.RemoveAll(tmpRoot)
					return fmt.Errorf("step %d/%d: RUN failed: %w", stepNum, totalSteps, err)
				}
				if exitCode != 0 {
					os.RemoveAll(tmpRoot)
					return fmt.Errorf("step %d/%d: RUN command exited with code %d", stepNum, totalSteps, exitCode)
				}

				tarBytes, digest, err := layer.CreateRunLayer(tmpRoot, beforeSnapshot)
				if err != nil {
					os.RemoveAll(tmpRoot)
					return fmt.Errorf("step %d/%d: create RUN layer: %w", stepNum, totalSteps, err)
				}

				if err := layer.StoreTar(tarBytes, digest); err != nil {
					os.RemoveAll(tmpRoot)
					return fmt.Errorf("step %d/%d: store RUN layer: %w", stepNum, totalSteps, err)
				}

				layerDigest = digest
				os.RemoveAll(tmpRoot)

				if !opts.NoCache {
					cache.Store(cacheKey, digest)
				}
			}

			layerSize := int64(0)
			if fi, err := os.Stat(store.LayerPath(layerDigest)); err == nil {
				layerSize = fi.Size()
			}

			layers = append(layers, manifest.Layer{
				Digest:    layerDigest,
				Size:      layerSize,
				CreatedBy: instr.FullText,
			})
			prevDigest = layerDigest

			elapsed := time.Since(stepStart)
			if cacheHit {
				fmt.Printf("Step %d/%d : %s [CACHE HIT] %.2fs\n", stepNum, totalSteps, instr.FullText, elapsed.Seconds())
			} else {
				fmt.Printf("Step %d/%d : %s [CACHE MISS] %.2fs\n", stepNum, totalSteps, instr.FullText, elapsed.Seconds())
			}
		}
	}

	// Build ENV list for manifest config.
	var envList []string
	envKeys := make([]string, 0, len(envState))
	for k := range envState {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	for _, k := range envKeys {
		envList = append(envList, k+"="+envState[k])
	}

	created := time.Now().UTC().Format(time.RFC3339)
	if allCacheHit && existingCreated != "" {
		created = existingCreated
	}

	m := &manifest.Manifest{
		Name:    opts.Name,
		Tag:     opts.Tag,
		Created: created,
		Config: manifest.Config{
			Env:        envList,
			Cmd:        cmdArgs,
			WorkingDir: workdir,
		},
		Layers: layers,
	}

	if err := store.SaveManifest(m); err != nil {
		return fmt.Errorf("save manifest: %w", err)
	}

	totalElapsed := time.Since(buildStart)
	fmt.Printf("Successfully built %s %s:%s (%.2fs)\n", manifest.ShortID(m), opts.Name, opts.Tag, totalElapsed.Seconds())

	return nil
}
