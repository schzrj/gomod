// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package modconv

import (
	"fmt"
	"gomod/modfile"
	"strings"
)

// ConvertLegacyConfig converts legacy config to modfile.
// The file argument is slash-delimited.
func ConvertLegacyConfig(f *modfile.File, file string, data []byte) error {
	i := strings.LastIndex(file, "/")
	j := -2
	if i >= 0 {
		j = strings.LastIndex(file[:i], "/")
	}
	convert := Converters[file[i+1:]]
	if convert == nil && j != -2 {
		convert = Converters[file[j+1:]]
	}
	if convert == nil {
		return fmt.Errorf("unknown legacy config file %s", file)
	}
	mf, err := convert(file, data)
	if err != nil {
		return fmt.Errorf("parsing %s: %v", file, err)
	}

	for _, r := range mf.Require {
		f.AddNewRequire(r.Mod.Path, r.Mod.Version, false)
	}
	for _, r := range mf.Replace {
		err := f.AddReplace(r.Old.Path, r.Old.Version, r.New.Path, r.New.Version)
		if err != nil {
			return fmt.Errorf("add replace: %v", err)
		}
	}

	return nil
}
