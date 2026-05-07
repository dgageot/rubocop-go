package runner

import (
	"encoding/json"
	"io"
	"os"
	"strings"

	"github.com/dgageot/rubocop-go/cop"
)

// Reporter consumes events from a Run and produces output. Implementations
// must be safe to use from a single goroutine.
//
// Lifecycle, in order:
//
//	r.Start(numCops)
//	r.FileFinished(filename, fileOffenses) // for every inspected file
//	r.Finish(allOffenses, filesInspected)
type Reporter interface {
	// Start is called once before any file is inspected.
	Start(numCops int)

	// FileFinished is called once per inspected file, after every cop has
	// run on it, with the offenses produced for that file. It is suitable
	// for emitting a per-file progress indicator.
	FileFinished(filename string, offenses []cop.Offense)

	// Finish is called once after all files have been inspected.
	Finish(allOffenses []cop.Offense, filesInspected int)
}

// TextReporter is the default human-readable reporter: a "rubocop-style" line
// with progress dots and coloured offense listings.
type TextReporter struct {
	Out io.Writer
}

// NewTextReporter returns a TextReporter writing to w (defaults to os.Stdout
// when w is nil).
func NewTextReporter(w io.Writer) *TextReporter {
	if w == nil {
		w = os.Stdout
	}
	return &TextReporter{Out: w}
}

// Start prints the inspection banner.
func (r *TextReporter) Start(numCops int) {
	writef(r.Out, "Inspecting Go files with %d cop(s)\n", numCops)
}

// FileFinished prints a single progress character for the file.
func (r *TextReporter) FileFinished(_ string, offenses []cop.Offense) {
	if len(offenses) > 0 {
		severity := maxSeverity(offenses)
		writef(r.Out, "%s%s%s", severity.Color(), severity.String(), resetColor)
	} else {
		writef(r.Out, ".")
	}
}

// Finish prints the offense listing and a summary line.
func (r *TextReporter) Finish(allOffenses []cop.Offense, filesInspected int) {
	writef(r.Out, "\n")

	if len(allOffenses) > 0 {
		writef(r.Out, "\nOffenses:\n\n")
		for _, o := range allOffenses {
			r.printOffense(o)
		}
		writef(r.Out, "\n")
	}

	if len(allOffenses) == 0 {
		writef(r.Out, "%d file(s) inspected, \033[32mno offenses\033[0m detected\n", filesInspected)
	} else {
		writef(r.Out, "%d file(s) inspected, %s%d offense(s)%s detected\n",
			filesInspected, maxSeverity(allOffenses).Color(), len(allOffenses), resetColor)
	}
}

func (r *TextReporter) printOffense(o cop.Offense) {
	writef(r.Out, "%s:%d:%d: %s%s%s: %s%s%s: %s\n",
		o.Pos.Filename, o.Pos.Line, o.Pos.Column,
		o.Severity.Color(), o.Severity.String(), resetColor,
		o.Severity.Color(), o.CopName, resetColor,
		o.Message,
	)

	// Print source context.
	line, err := readLine(o.Pos.Filename, o.Pos.Line)
	if err == nil {
		writef(r.Out, "%s\n", line)
		underline := strings.Repeat(" ", o.Pos.Column-1)
		length := o.End.Column - o.Pos.Column
		if length <= 0 {
			length = 1
		}
		writef(r.Out, "%s%s%s%s\n", underline, o.Severity.Color(), strings.Repeat("^", length), resetColor)
	}
}

// JSONReporter emits a single JSON document at Finish containing every
// offense. Suitable for CI integrations that want machine-readable output.
type JSONReporter struct {
	Out io.Writer
}

// NewJSONReporter returns a JSONReporter writing to w (defaults to os.Stdout
// when w is nil).
func NewJSONReporter(w io.Writer) *JSONReporter {
	if w == nil {
		w = os.Stdout
	}
	return &JSONReporter{Out: w}
}

// Start is a no-op.
func (*JSONReporter) Start(int) {}

// FileFinished is a no-op.
func (*JSONReporter) FileFinished(string, []cop.Offense) {}

// Finish writes a single JSON object describing the run.
func (r *JSONReporter) Finish(allOffenses []cop.Offense, filesInspected int) {
	type jsonOffense struct {
		Cop      string `json:"cop"`
		Severity string `json:"severity"`
		Message  string `json:"message"`
		File     string `json:"file"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
		EndLine  int    `json:"end_line"`
		EndCol   int    `json:"end_column"`
	}
	out := struct {
		FilesInspected int           `json:"files_inspected"`
		OffenseCount   int           `json:"offense_count"`
		Offenses       []jsonOffense `json:"offenses"`
	}{
		FilesInspected: filesInspected,
		OffenseCount:   len(allOffenses),
		Offenses:       make([]jsonOffense, 0, len(allOffenses)),
	}
	for _, o := range allOffenses {
		out.Offenses = append(out.Offenses, jsonOffense{
			Cop:      o.CopName,
			Severity: severityName(o.Severity),
			Message:  o.Message,
			File:     o.Pos.Filename,
			Line:     o.Pos.Line,
			Column:   o.Pos.Column,
			EndLine:  o.End.Line,
			EndCol:   o.End.Column,
		})
	}
	enc := json.NewEncoder(r.Out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		writef(r.Out, "json encode error: %v\n", err)
	}
}

func severityName(s cop.Severity) string {
	switch s {
	case cop.Convention:
		return "convention"
	case cop.Warning:
		return "warning"
	case cop.Error:
		return "error"
	default:
		return "unknown"
	}
}
