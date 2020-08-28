package main

import (
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/Masterminds/semver"
	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const usage = "modtransplant -dest=<destination-file> -src=<source-file> [-force-overwrite]"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	var (
		destFile       string
		srcFile        string
		forceOverwrite bool
	)
	fs := flag.NewFlagSet("modtransplant", flag.ExitOnError)
	fs.StringVar(&destFile, "dest", "", "destination go.mod file")
	fs.StringVar(&srcFile, "src", "", "source go.mod file")
	fs.BoolVar(&forceOverwrite, "force-overwrite", false, "force overwrite of versions of matching module paths")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	if destFile == "" || srcFile == "" {
		return errors.New(usage)
	}

	destContent, err := ioutil.ReadFile(destFile)
	if err != nil {
		return err
	}
	dest, err := modfile.Parse(destFile, destContent, nil)
	if err != nil {
		return err
	}

	sourceContent, err := ioutil.ReadFile(srcFile)
	if err != nil {
		return err
	}
	src, err := modfile.Parse(srcFile, sourceContent, nil)
	if err != nil {
		return err
	}

	if err := mergeRequires(dest, src, forceOverwrite); err != nil {
		return err
	}
	if err := mergeReplacements(dest, src); err != nil {
		return err
	}
	if err := mergeExcludes(dest, src); err != nil {
		return err
	}

	dest.Cleanup()
	out, err := dest.Format()
	if err != nil {
		return err
	}
	fmt.Println(string(out))

	return nil
}

// mergeRequires merges "require" statements into the destination.
//
// Mutation Rules:
// - Module paths missing from the destination entirely will be added.
// - Module paths in the destination that have mismatched versions will be
// overwritten by what's in the source.
// - Module paths that are indirect in the destination, but direct in the source
// will be made direct.
// - Any dependency that the destination has on the source will be removed.
func mergeRequires(dest, src *modfile.File, forceOverwrite bool) error {
	if err := dest.DropRequire(src.Module.Mod.Path); err != nil {
		return err
	}

	for _, srcR := range src.Require {
		var found bool
		for _, destR := range dest.Require {
			if srcR.Mod.String() == destR.Mod.String() {
				fmt.Fprintf(os.Stderr, "(require) match: %s\n", srcR.Mod)
				found = true
				break
			}
			if srcR.Mod.Path == destR.Mod.Path {
				if srcR.Mod.Version != destR.Mod.Version {
					destVersion, err := semver.NewVersion(destR.Mod.Version)
					if err != nil {
						return err
					}
					srcVersion, err := semver.NewVersion(srcR.Mod.Version)
					if err != nil {
						return err
					}

					if forceOverwrite {
						destR.Mod.Version = srcR.Mod.Version
					} else {
						if !canCompare(destVersion, srcVersion) {
							return fmt.Errorf("cannot reconcile difference between versions: dest=%s src=%s", destR.Mod, srcR.Mod)
						}
						if srcVersion.LessThan(destVersion) {
							fmt.Fprintf(os.Stderr, "(require) replace version: %s %s -> %s\n", destR.Mod.Path, destR.Mod.Version, srcR.Mod.Version)
							destR.Mod.Version = srcR.Mod.Version
						}
					}
				}
				if destR.Indirect != !srcR.Indirect {
					fmt.Fprintf(os.Stderr, "(require) make direct: %s\n", srcR.Mod)
					destR.Indirect = srcR.Indirect
				}
			}
		}

		if !found {
			fmt.Fprintf(os.Stderr, "(require) add new: %s (%s)\n", srcR.Mod.String(), indirectStr(srcR.Indirect))
			dest.AddNewRequire(srcR.Mod.Path, srcR.Mod.Version, srcR.Indirect)
		}
	}

	return nil
}

// mergeReplacements merges "replace" statements into the destination.
//
// Mutation rules:
// - Module paths missing from the destination entirely will be added.
// - Replacements for the source module in the destination will be removed.
//
// This function will error if matching module paths are found in both the
// source and destination, but the versions mismatch. This is considered a
// condition that will need human intervention.
func mergeReplacements(dest, src *modfile.File) error {
	var dropVersions []module.Version
	for _, r := range dest.Replace {
		if r.Old.Path == src.Module.Mod.Path {
			dropVersions = append(dropVersions, r.Old)
		}
	}
	for _, v := range dropVersions {
		fmt.Fprintf(os.Stderr, "drop replacement: %s\n", v.String())
		dest.DropReplace(v.Path, v.Version)
	}

	for _, srcR := range src.Replace {
		var found bool
		for _, destR := range dest.Replace {
			if srcR.Old.String() == destR.Old.String() {
				fmt.Fprintf(os.Stderr, "(replace) match: %s\n", srcR.Old)
				found = true
				break
			}
			if srcR.Old.Path == destR.Old.Path && srcR.Old.Version == destR.Old.Version {
				if srcR.New.Path != destR.New.Path || srcR.New.Version != destR.New.Version {
					return errors.New("(replace) source and destination old path/version match, but new path/version do not")
				}
			}
		}

		if !found {
			fmt.Fprintf(os.Stderr, "(replace) add new: %s -> %s\n", srcR.Old, srcR.New)
			dest.AddReplace(srcR.Old.Path, srcR.Old.Version, srcR.New.Path, srcR.New.Version)
		}
	}

	return nil
}

// mergeExcludes merges "exclude" statements into the destination. Only
// exclusions missing from the destination will be added.
func mergeExcludes(dest, src *modfile.File) error {
	for _, srcE := range src.Exclude {
		var found bool
		for _, destE := range dest.Exclude {
			if srcE.Mod.String() == destE.Mod.String() {
				fmt.Fprintf(os.Stderr, "(replace) match: %s\n", srcE.Mod)
				found = true
				break
			}
		}

		if !found {
			fmt.Fprintf(os.Stderr, "(exclude) add new: %s -> %s\n", srcE.Mod)
			dest.AddExclude(srcE.Mod.Path, srcE.Mod.Version)
		}
	}

	return nil
}

func indirectStr(indirect bool) string {
	if indirect {
		return "indirect"
	}
	return "direct"
}

func canCompare(a, b *semver.Version) bool {
	return (a.Prerelease() == "" && b.Prerelease() == "") || (a.Prerelease() != "" && b.Prerelease() != "")
}
