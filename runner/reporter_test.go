package runner_test

import (
	"bytes"
	"encoding/json"
	"go/token"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/dgageot/rubocop-go/cop"
	"github.com/dgageot/rubocop-go/runner"
)

func TestTextReporter_NoOffenses(t *testing.T) {
	var buf bytes.Buffer
	r := runner.NewTextReporter(&buf)

	r.Start(3)
	r.FileFinished("a.go", nil)
	r.Finish(nil, 1)

	out := buf.String()
	assert.Contains(t, out, "Inspecting Go files with 3 cop(s)")
	assert.Contains(t, out, ".")
	assert.Contains(t, out, "no offenses")
}

func TestJSONReporter(t *testing.T) {
	var buf bytes.Buffer
	r := runner.NewJSONReporter(&buf)

	off := cop.Offense{
		Pos:      token.Position{Filename: "x.go", Line: 4, Column: 2},
		End:      token.Position{Filename: "x.go", Line: 4, Column: 8},
		Message:  "bad",
		CopName:  "Lint/Bad",
		Severity: cop.Error,
	}

	r.Start(1)
	r.FileFinished("x.go", []cop.Offense{off})
	r.Finish([]cop.Offense{off}, 1)

	var got struct {
		FilesInspected int `json:"files_inspected"`
		OffenseCount   int `json:"offense_count"`
		Offenses       []struct {
			Cop      string `json:"cop"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
		} `json:"offenses"`
	}
	require.NoError(t, json.Unmarshal(buf.Bytes(), &got))

	assert.Equal(t, 1, got.FilesInspected)
	assert.Equal(t, 1, got.OffenseCount)
	require.Len(t, got.Offenses, 1)
	assert.Equal(t, "Lint/Bad", got.Offenses[0].Cop)
	assert.Equal(t, "error", got.Offenses[0].Severity)
	assert.Equal(t, "bad", got.Offenses[0].Message)
}
