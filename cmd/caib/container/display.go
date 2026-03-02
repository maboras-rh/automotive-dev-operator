/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package container

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
)

// ColorFormatter provides consistent color formatting across container commands
type ColorFormatter struct {
	LabelColor   func(...any) string
	ValueColor   func(...any) string
	CommandColor func(...any) string
}

// NewColorFormatter creates a ColorFormatter with appropriate color settings
func NewColorFormatter() *ColorFormatter {
	if supportsColorOutput() {
		return &ColorFormatter{
			LabelColor:   color.New(color.FgHiWhite, color.Bold).SprintFunc(),
			ValueColor:   color.New(color.FgHiGreen).SprintFunc(),
			CommandColor: color.New(color.FgHiYellow, color.Bold).SprintFunc(),
		}
	}

	// No-color fallback
	noColor := func(a ...any) string { return fmt.Sprint(a...) }
	return &ColorFormatter{
		LabelColor:   noColor,
		ValueColor:   noColor,
		CommandColor: noColor,
	}
}

// supportsColorOutput determines if the terminal supports color output.
// color.NoColor already reflects NO_COLOR, TERM=dumb, and non-TTY; we only add a mono check.
func supportsColorOutput() bool {
	if color.NoColor {
		return false
	}
	termType := strings.ToLower(os.Getenv("TERM"))
	return !strings.Contains(termType, "mono")
}
