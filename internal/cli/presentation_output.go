package cli

import (
	"bytes"
	"io"

	"github.com/jinyongp/devtools/internal/cli/presentation"
	"github.com/jinyongp/devtools/internal/cliui"
	"github.com/jinyongp/devtools/internal/protocol"
)

type outputController struct {
	app             *App
	streams         IO
	environment     presentation.Environment
	decision        presentation.Decision
	operation       string
	invocation      protocol.InvocationContext
	requestID       string
	handlerReturned bool
}

func (o *outputController) outputError(code, message string) *protocol.Error {
	effects, retry := "none", "after_dependency_change"
	if o.handlerReturned {
		effects, retry = "unknown", "inspect_first"
	}
	details := map[string]any{"phase": "render", "effects": effects, "retry": retry}
	if o.requestID != "" {
		details["request_id"] = o.requestID
	}
	return protocol.NewError(code, message, 1, details)
}

func (o *outputController) prepareError(e *protocol.Error) *protocol.Error {
	if e.Details == nil {
		e.Details = map[string]any{}
	}
	e.Details = protocol.BindGuidance(e.Details, o.invocation).(map[string]any)
	return protocol.Public(e, o.operation)
}

func (o *outputController) failure(e *protocol.Error) int {
	e = o.prepareError(e)
	var public bytes.Buffer
	if protocol.Failure(&public, e) != nil {
		e = o.prepareError(o.outputError("io_error", "Cannot write output."))
		public.Reset()
		if protocol.Failure(&public, e) != nil {
			return 1
		}
	}
	return o.publish(public.Bytes(), e.ExitCode, true)
}

func (o *outputController) success(data any) int {
	var public bytes.Buffer
	if protocol.Success(&public, protocol.BindGuidance(data, o.invocation)) != nil {
		return o.failure(o.outputError("io_error", "Cannot write output."))
	}
	return o.publish(public.Bytes(), 0, false)
}

func (o *outputController) publish(public []byte, exit int, failure bool) int {
	data := public
	if o.decision.Format == presentation.FormatText {
		build, render := o.app.buildDocument, o.app.renderDocument
		if build == nil {
			build = func(command string, public []byte) (cliui.Document, error) {
				return presentation.BuildWithInputs(command, public, o.app.presentationInputs(command))
			}
		}
		if render == nil {
			render = cliui.Render
		}
		document, err := build(o.operation, public)
		if err == nil {
			data, err = render(document, textOptions(o.environment, failure))
		}
		if err != nil {
			document, err = presentation.BuildGeneric(public)
			if err == nil {
				data, err = render(document, textOptions(o.environment, failure))
			}
		}
		if err != nil {
			if !failure {
				// Do not recurse through a renderer that just failed twice.
				e := o.prepareError(o.outputError("internal_error", "Cannot render output."))
				var encoded bytes.Buffer
				if protocol.Failure(&encoded, e) != nil {
					return 1
				}
				public, exit, failure = encoded.Bytes(), 1, true
			}
			data = []byte("ERROR\nDiagnostic (escaped JSON)\n" + cliui.Escape(string(public)))
		}
	}
	writer := o.streams.Out
	if failure {
		writer = o.streams.Err
	}
	n, err := writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	if err != nil {
		if failure {
			return 1 // stderr transport failure has priority; never retry it.
		}
		return o.failure(o.outputError("io_error", "Cannot write output."))
	}
	return exit
}

func (a *App) presentationInputs(name string) []presentation.InputHint {
	for _, command := range a.commands {
		if command.Name != name {
			continue
		}
		var inputs []presentation.InputHint
		for _, argument := range command.Arguments {
			rule, _ := inputRule(argument.Pattern, 0, 0, 0)
			inputs = append(inputs, presentation.InputHint{Name: argument.Name, Choices: patternChoices(argument.Pattern), Rule: rule})
		}
		for _, option := range command.Options {
			rule, _ := inputRule(option.Pattern, option.MinLength, option.MaxLength, 0)
			inputs = append(inputs, presentation.InputHint{Name: option.Name, Option: true, Choices: patternChoices(option.Pattern), Rule: rule})
		}
		return inputs
	}
	return nil
}
