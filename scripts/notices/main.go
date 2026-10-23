// Command notices collects license texts for compiled release dependencies.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

type targetList []string

func (values *targetList) String() string {
	return strings.Join(*values, ",")
}

func (values *targetList) Set(value string) error {
	parts := strings.Split(value, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("target must use GOOS/GOARCH: %q", value)
	}
	*values = append(*values, value)
	return nil
}

type moduleInfo struct {
	Path    string      `json:"Path"`
	Version string      `json:"Version"`
	Dir     string      `json:"Dir"`
	Main    bool        `json:"Main"`
	Replace *moduleInfo `json:"Replace"`
}

type packageInfo struct {
	ImportPath string      `json:"ImportPath"`
	Dir        string      `json:"Dir"`
	Standard   bool        `json:"Standard"`
	Module     *moduleInfo `json:"Module"`
}

type moduleUse struct {
	module      moduleInfo
	packageDirs map[string]struct{}
}

type notice struct {
	name string
	path string
}

type component struct {
	name    string
	notices []notice
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var output string
	var targets targetList
	flag.StringVar(&output, "output", "", "new THIRD_PARTY_NOTICES.txt path")
	flag.Var(&targets, "target", "compiled GOOS/GOARCH target")
	flag.Parse()
	if output == "" || len(targets) == 0 || flag.NArg() != 0 {
		return errors.New("usage: notices -output PATH -target GOOS/GOARCH [...]")
	}
	if _, err := os.Stat(output); err == nil {
		return fmt.Errorf("output path already exists: %s", output)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect output path: %w", err)
	}

	root, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("get repository root: %w", err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		return fmt.Errorf("run notices from the repository root: %w", err)
	}
	targets = uniqueSorted(targets)
	modules := make(map[string]*moduleUse)
	standardDirs := make(map[string]struct{})
	for _, target := range targets {
		if err := collectTarget(root, target, modules, standardDirs); err != nil {
			return err
		}
	}

	components, err := buildComponents(modules, standardDirs)
	if err != nil {
		return err
	}
	content, err := render(targets, components)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(output, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create notice file: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(output)
		return fmt.Errorf("write notice file: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(output)
		return fmt.Errorf("close notice file: %w", err)
	}
	return nil
}

func collectTarget(root, target string, modules map[string]*moduleUse, standardDirs map[string]struct{}) error {
	parts := strings.Split(target, "/")
	command := exec.Command("go", "list", "-mod=readonly", "-deps", "-json", "./cmd/grand-lyon-mcp")
	command.Dir = root
	command.Env = environment(os.Environ(), map[string]string{
		"CGO_ENABLED": "0",
		"GOOS":        parts[0],
		"GOARCH":      parts[1],
	})
	data, err := command.Output()
	if err != nil {
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			return fmt.Errorf("list dependencies for %s: %s", target, strings.TrimSpace(string(exitError.Stderr)))
		}
		return fmt.Errorf("list dependencies for %s: %w", target, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var packageData packageInfo
		if err := decoder.Decode(&packageData); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return fmt.Errorf("decode dependency graph for %s: %w", target, err)
		}
		if packageData.Standard {
			if packageData.Dir != "" {
				standardDirs[packageData.Dir] = struct{}{}
			}
			continue
		}
		if packageData.Module == nil || packageData.Module.Main {
			continue
		}
		module := packageData.Module
		if module.Replace != nil {
			return fmt.Errorf("module replacement requires explicit notice handling: %s", module.Path)
		}
		if module.Path == "" || module.Version == "" || module.Dir == "" || packageData.Dir == "" {
			return fmt.Errorf("incomplete module metadata for package %s", packageData.ImportPath)
		}
		key := module.Path + "@" + module.Version
		usage, found := modules[key]
		if !found {
			usage = &moduleUse{module: *module, packageDirs: make(map[string]struct{})}
			modules[key] = usage
		}
		usage.packageDirs[packageData.Dir] = struct{}{}
	}
	return nil
}

func buildComponents(modules map[string]*moduleUse, standardDirs map[string]struct{}) ([]component, error) {
	goComponent, err := buildGoComponent(standardDirs)
	if err != nil {
		return nil, err
	}
	components := []component{goComponent}
	keys := make([]string, 0, len(modules))
	for key := range modules {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		usage := modules[key]
		notices, err := collectNotices(usage.module.Dir, usage.packageDirs)
		if err != nil {
			return nil, fmt.Errorf("collect notices for %s: %w", key, err)
		}
		if len(notices) == 0 {
			return nil, fmt.Errorf("no license or notice file found for compiled module %s", key)
		}
		components = append(components, component{name: key, notices: notices})
	}
	return components, nil
}

func buildGoComponent(packageDirs map[string]struct{}) (component, error) {
	gorootData, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return component{}, fmt.Errorf("get GOROOT: %w", err)
	}
	goroot := strings.TrimSpace(string(gorootData))
	licenseRoot := ""
	for _, candidate := range []string{goroot, filepath.Dir(goroot)} {
		if regularFile(filepath.Join(candidate, "LICENSE")) {
			licenseRoot = candidate
			break
		}
	}
	if licenseRoot == "" {
		return component{}, errors.New("Go LICENSE is absent from GOROOT and its parent")
	}
	noticeMap := make(map[string]string)
	noticeMap["LICENSE"] = filepath.Join(licenseRoot, "LICENSE")
	for _, candidate := range []string{filepath.Join(goroot, "PATENTS"), filepath.Join(licenseRoot, "PATENTS")} {
		if regularFile(candidate) {
			noticeMap["PATENTS"] = candidate
			break
		}
	}
	sourceRoot := filepath.Join(goroot, "src")
	for packageDir := range packageDirs {
		relativeDir, err := filepath.Rel(sourceRoot, packageDir)
		if err != nil || relativeDir == ".." || strings.HasPrefix(relativeDir, ".."+string(filepath.Separator)) {
			return component{}, fmt.Errorf("standard package directory is outside GOROOT/src: %s", packageDir)
		}
		for _, item := range noticeFilesBetween(sourceRoot, packageDir) {
			relative, err := filepath.Rel(goroot, item)
			if err != nil || strings.HasPrefix(relative, "..") {
				return component{}, fmt.Errorf("Go notice is outside GOROOT: %s", item)
			}
			noticeMap[filepath.ToSlash(relative)] = item
		}
	}
	return component{name: "Go toolchain and standard library " + runtime.Version(), notices: sortedNotices(noticeMap)}, nil
}

func collectNotices(moduleRoot string, packageDirs map[string]struct{}) ([]notice, error) {
	noticeMap := make(map[string]string)
	for packageDir := range packageDirs {
		relative, err := filepath.Rel(moduleRoot, packageDir)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, fmt.Errorf("package directory is outside module root: %s", packageDir)
		}
		for _, item := range noticeFilesBetween(moduleRoot, packageDir) {
			name, err := filepath.Rel(moduleRoot, item)
			if err != nil {
				return nil, err
			}
			noticeMap[filepath.ToSlash(name)] = item
		}
	}
	return sortedNotices(noticeMap), nil
}

func noticeFilesBetween(root, start string) []string {
	result := []string{}
	current := filepath.Clean(start)
	root = filepath.Clean(root)
	for {
		entries, err := os.ReadDir(current)
		if err == nil {
			for _, entry := range entries {
				name := strings.ToUpper(entry.Name())
				if !strings.HasPrefix(name, "LICENSE") && !strings.HasPrefix(name, "NOTICE") && !strings.HasPrefix(name, "COPYING") {
					continue
				}
				path := filepath.Join(current, entry.Name())
				if regularFile(path) {
					result = append(result, path)
				}
			}
		}
		if current == root {
			break
		}
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
		current = parent
	}
	return result
}

func sortedNotices(values map[string]string) []notice {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := make([]notice, 0, len(names))
	for _, name := range names {
		result = append(result, notice{name: name, path: values[name]})
	}
	return result
}

func render(targets []string, components []component) ([]byte, error) {
	var output bytes.Buffer
	output.WriteString("THIRD-PARTY SOFTWARE NOTICES\n\n")
	output.WriteString("Release targets:\n")
	for _, target := range targets {
		fmt.Fprintf(&output, "- %s\n", target)
	}
	output.WriteString("\nThese texts come from the compiled Go dependency graph.\n")
	for _, component := range components {
		output.WriteString("\n==============================================================================\n")
		fmt.Fprintf(&output, "Component: %s\n", component.name)
		for _, item := range component.notices {
			data, err := os.ReadFile(item.path)
			if err != nil {
				return nil, fmt.Errorf("read %s notice %s: %w", component.name, item.name, err)
			}
			fmt.Fprintf(&output, "\n--- %s ---\n", item.name)
			output.Write(data)
			if len(data) == 0 || data[len(data)-1] != '\n' {
				output.WriteByte('\n')
			}
		}
	}
	return output.Bytes(), nil
}

func environment(current []string, values map[string]string) []string {
	result := make([]string, 0, len(current)+len(values))
	for _, item := range current {
		name, _, _ := strings.Cut(item, "=")
		if _, replaced := values[name]; !replaced {
			result = append(result, item)
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, key+"="+values[key])
	}
	return result
}

func uniqueSorted(values []string) []string {
	seen := make(map[string]struct{})
	for _, value := range values {
		seen[value] = struct{}{}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
