package surveyext

// This file extends survey.Editor to give it more flexible behavior. For more context, read
// https://github.com/cli/cli/issues/70
// To see what we extended, search through for EXTENDED comments.

import (
	"bytes"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/AlecAivazis/survey/v2"
	"github.com/AlecAivazis/survey/v2/terminal"
	shellquote "github.com/kballard/go-shellquote"
)

var (
	bom    = []byte{0xef, 0xbb, 0xbf}
	editor = "nano" // EXTENDED to switch from vim as a default editor
)

func init() {
	if runtime.GOOS == "windows" {
		editor = "notepad"
	} else if g := os.Getenv("GIT_EDITOR"); g != "" {
		editor = g
	} else if v := os.Getenv("VISUAL"); v != "" {
		editor = v
	} else if e := os.Getenv("EDITOR"); e != "" {
		editor = e
	}
}

// EXTENDED to enable different prompting behavior
type GhEditor struct {
	*survey.Editor
}

// EXTENDED to change prompt text
var EditorQuestionTemplate = `
{{- if .ShowHelp }}{{- color .Config.Icons.Help.Format }}{{ .Config.Icons.Help.Text }} {{ .Help }}{{color "reset"}}{{"\n"}}{{end}}
{{- color .Config.Icons.Question.Format }}{{ .Config.Icons.Question.Text }} {{color "reset"}}
{{- color "default+hb"}}{{ .Message }} {{color "reset"}}
{{- if .ShowAnswer}}
  {{- color "cyan"}}{{.Answer}}{{color "reset"}}{{"\n"}}
{{- else }}
  {{- if and .Help (not .ShowHelp)}}{{color "cyan"}}[{{ .Config.HelpInput }} for help]{{color "reset"}} {{end}}
  {{- if and .Default (not .HideDefault)}}{{color "white"}}({{.Default}}) {{color "reset"}}{{end}}
	{{- color "cyan"}}[(e) to launch {{ .EditorName }}, enter to skip] {{color "reset"}}
{{- end}}`

// EXTENDED to pass editor name (to use in prompt)
type EditorTemplateData struct {
	survey.Editor
	EditorName string
	Answer     string
	ShowAnswer bool
	ShowHelp   bool
	Config     *survey.PromptConfig
}

// EXTENDED to augment prompt text and keypress handling
func (e *GhEditor) prompt(initialValue string, config *survey.PromptConfig) (interface{}, error) {
	err := e.Render(
		EditorQuestionTemplate,
		// EXTENDED to support printing editor in prompt
		EditorTemplateData{
			Editor:     *e.Editor,
			EditorName: filepath.Base(editor),
			Config:     config,
		},
	)
	if err != nil {
		return "", err
	}

	// start reading runes from the standard in
	rr := e.NewRuneReader()
	rr.SetTermMode()
	defer rr.RestoreTermMode()

	cursor := e.NewCursor()
	cursor.Hide()
	defer cursor.Show()

	for {
		// EXTENDED to handle the e to edit / enter to skip behavior
		r, _, err := rr.ReadRune()
		if err != nil {
			return "", err
		}
		if r == 'e' {
			break
		}
		if r == '\r' || r == '\n' {
			return "", nil
		}
		if r == terminal.KeyInterrupt {
			return "", terminal.InterruptErr
		}
		if r == terminal.KeyEndTransmission {
			break
		}
		if string(r) == config.HelpInput && e.Help != "" {
			err = e.Render(
				EditorQuestionTemplate,
				EditorTemplateData{
					// EXTENDED to support printing editor in prompt
					Editor:     *e.Editor,
					EditorName: filepath.Base(editor),
					ShowHelp:   true,
					Config:     config,
				},
			)
			if err != nil {
				return "", err
			}
		}
		continue
	}

	// prepare the temp file
	pattern := e.FileName
	if pattern == "" {
		pattern = "survey*.txt"
	}
	f, err := ioutil.TempFile("", pattern)
	if err != nil {
		return "", err
	}
	defer os.Remove(f.Name())

	// write utf8 BOM header
	// The reason why we do this is because notepad.exe on Windows determines the
	// encoding of an "empty" text file by the locale, for example, GBK in China,
	// while golang string only handles utf8 well. However, a text file with utf8
	// BOM header is not considered "empty" on Windows, and the encoding will then
	// be determined utf8 by notepad.exe, instead of GBK or other encodings.
	if _, err := f.Write(bom); err != nil {
		return "", err
	}

	// write initial value
	if _, err := f.WriteString(initialValue); err != nil {
		return "", err
	}

	// close the fd to prevent the editor unable to save file
	if err := f.Close(); err != nil {
		return "", err
	}

	stdio := e.Stdio()

	args, err := shellquote.Split(editor)
	if err != nil {
		return "", err
	}
	args = append(args, f.Name())

	// open the editor
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = stdio.In
	cmd.Stdout = stdio.Out
	cmd.Stderr = stdio.Err
	cursor.Show()
	if err := cmd.Run(); err != nil {
		return "", err
	}

	// raw is a BOM-unstripped UTF8 byte slice
	raw, err := ioutil.ReadFile(f.Name())
	if err != nil {
		return "", err
	}

	// strip BOM header
	text := string(bytes.TrimPrefix(raw, bom))

	// check length, return default value on empty
	if len(text) == 0 && !e.AppendDefault {
		return e.Default, nil
	}

	return text, nil
}

// EXTENDED This is straight copypasta from survey to get our overridden prompt called.;
func (e *GhEditor) Prompt(config *survey.PromptConfig) (interface{}, error) {
	initialValue := ""
	if e.Default != "" && e.AppendDefault {
		initialValue = e.Default
	}
	return e.prompt(initialValue, config)
}
