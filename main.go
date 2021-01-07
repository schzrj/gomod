package main

import (
	"encoding/json"
	"fmt"
	"go/build"
	"gomod/base"
	"gomod/cfg"
	"gomod/modconv"
	"gomod/modfile"
	"gomod/module"
	"gomod/renameio"
	"gomod/search"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

var altConfigs = []string{
	"Gopkg.lock",

	"GLOCKFILE",
	"Godeps/Godeps.json",
	"dependencies.tsv",
	"glide.lock",
	"vendor.conf",
	"vendor.yml",
	"vendor/manifest",
	"vendor/vendor.json",

	".git/config",
}
var (
	modRoot      string
	modFile      *modfile.File
	CmdModModule string
	Target       module.Version
	initialized  bool
	modName      string
)

// WriteGoMod writes the current build list back to go.mod.
func WriteGoMod() {
	// If we aren't in a module, we don't have anywhere to write a go.mod file.
	if modRoot == "" {
		return
	}

	modFile.Cleanup() // clean file after edits
	new, err := modFile.Format()
	if err != nil {
		base.Fatalf("go: %v", err)
	}

	file := filepath.Join(modRoot, modName)

	if err := renameio.WriteFile(file, new); err != nil {
		base.Fatalf("error writing go.mod: %v", err)
	}
}

// Exported only for testing.
func FindModulePath(dir string) (string, error) {
	if CmdModModule != "" {
		// Running go mod init x/y/z; return x/y/z.
		return CmdModModule, nil
	}

	// Cast about for import comments,
	// first in top-level directory, then in subdirectories.
	list, _ := ioutil.ReadDir(dir)
	for _, info := range list {
		if info.Mode().IsRegular() && strings.HasSuffix(info.Name(), ".go") {
			if com := findImportComment(filepath.Join(dir, info.Name())); com != "" {
				return com, nil
			}
		}
	}
	for _, info1 := range list {
		if info1.IsDir() {
			files, _ := ioutil.ReadDir(filepath.Join(dir, info1.Name()))
			for _, info2 := range files {
				if info2.Mode().IsRegular() && strings.HasSuffix(info2.Name(), ".go") {
					if com := findImportComment(filepath.Join(dir, info1.Name(), info2.Name())); com != "" {
						return path.Dir(com), nil
					}
				}
			}
		}
	}

	// Look for Godeps.json declaring import path.
	data, _ := ioutil.ReadFile(filepath.Join(dir, "Godeps/Godeps.json"))
	var cfg1 struct{ ImportPath string }
	json.Unmarshal(data, &cfg1)
	if cfg1.ImportPath != "" {
		return cfg1.ImportPath, nil
	}

	// Look for vendor.json declaring import path.
	data, _ = ioutil.ReadFile(filepath.Join(dir, "vendor/vendor.json"))
	var cfg2 struct{ RootPath string }
	json.Unmarshal(data, &cfg2)
	if cfg2.RootPath != "" {
		return cfg2.RootPath, nil
	}

	// Look for path in GOPATH.
	for _, gpdir := range filepath.SplitList(cfg.BuildContext.GOPATH) {
		if gpdir == "" {
			continue
		}
		if rel := search.InDir(dir, filepath.Join(gpdir, "src")); rel != "" && rel != "." {
			return filepath.ToSlash(rel), nil
		}
	}

	// Look for .git/config with github origin as last resort.
	data, _ = ioutil.ReadFile(filepath.Join(dir, ".git/config"))
	if m := gitOriginRE.FindSubmatch(data); m != nil {
		return "github.com/" + string(m[1]), nil
	}

	return "", fmt.Errorf("cannot determine module path for source directory %s (outside GOPATH, no import comments)", dir)
}

var (
	gitOriginRE     = regexp.MustCompile(`(?m)^\[remote "origin"\]\r?\n\turl = (?:https://github.com/|git@github.com:|gh:|https://hub.fastgit.org)([^/]+/[^/]+?)(\.git)?\r?\n`)
	importCommentRE = regexp.MustCompile(`(?m)^package[ \t]+[^ \t\r\n/]+[ \t]+//[ \t]+import[ \t]+(\"[^"]+\")[ \t]*\r?\n`)
)

func findImportComment(file string) string {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return ""
	}
	m := importCommentRE.FindSubmatch(data)
	if m == nil {
		return ""
	}
	path, err := strconv.Unquote(string(m[1]))
	if err != nil {
		return ""
	}
	return path
}
func legacyModInit() {
	if modFile == nil {
		path, err := FindModulePath(modRoot)
		if err != nil {
			base.Fatalf("go: %v", err)
		}
		fmt.Fprintf(os.Stderr, "go: creating new go.mod: module %s\n", path)
		modFile = new(modfile.File)
		modFile.AddModuleStmt(path)
	}

	addGoStmt()

	for _, name := range altConfigs {
		cfg := filepath.Join(modRoot, name)
		data, err := ioutil.ReadFile(cfg)
		if err == nil {
			convert := modconv.Converters[name]
			if convert == nil {
				return
			}
			fmt.Fprintf(os.Stderr, "go: copying requirements from %s\n", base.ShortPath(cfg))
			cfg = filepath.ToSlash(cfg)
			if err := modconv.ConvertLegacyConfig(modFile, cfg, data); err != nil {
				base.Fatalf("go: %v", err)
			}
			if len(modFile.Syntax.Stmt) == 1 {
				// Add comment to avoid re-converting every time it runs.
				modFile.AddComment("// go: no requirements found in " + name)
			}
			initialized = true
			return
		}
	}
}

// addGoStmt adds a go statement referring to the current version.
func addGoStmt() {
	tags := build.Default.ReleaseTags
	version := tags[len(tags)-1]
	if !strings.HasPrefix(version, "go") || !modfile.GoVersionRE.MatchString(version[2:]) {
		base.Fatalf("go: unrecognized default version %q", version)
	}
	if err := modFile.AddGoStmt(version[2:]); err != nil {
		base.Fatalf("go: internal error: %v", err)
	}
}

var RootCmd = &cobra.Command{
	Use:   "gomod <command>",
	Short: "`gomod` only init and create go.mod but not download package",
	Long:  "`gomod` only init and create go.mod but not download package",
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Usage()
	},
}
var initCmd = &cobra.Command{
	Use:   "init <module>",
	Short: "`init` initialize new module in current directory",
	Long: `
Init initializes and writes a new go.mod file in the current directory, in
effect creating a new module rooted at the current directory. The go.mod file
must not already exist.

Init accepts one optional argument, the module path for the new module. If the
module path argument is omitted, init will attempt to infer the module path
using import comments in .go files, vendoring tool configuration files (like
Gopkg.lock), and the current directory (if in GOPATH).

If a configuration file for a vendoring tool is present, init will attempt to
import module requirements from it.
`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) > 0 {
			CmdModModule = args[0]
		}
		legacyModInit()
		WriteGoMod()
	},
}

func init() {
	RootCmd.AddCommand(initCmd)
	initCmd.Flags().StringVar(&modRoot, "modDir", ".", "mod file dir")
	initCmd.Flags().StringVar(&modName, "modName", "go.mod", "mod file name")
}
func main() {
	RootCmd.Execute()
	if initialized {
		bytes, err := modFile.Format()
		if err == nil {
			fmt.Println(fmt.Sprintf("%v", string(bytes)))
		}
	}
}
