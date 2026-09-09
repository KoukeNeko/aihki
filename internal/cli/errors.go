package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/KoukeNeko/taiga-cli/internal/output"
	"github.com/KoukeNeko/taiga-cli/internal/taiga"
	"github.com/spf13/cobra"
)

const (
	ExitSuccess              = 0
	ExitGeneric              = 1
	ExitUsage                = 2
	ExitAuth                 = 3
	ExitForbidden            = 4
	ExitNotFound             = 5
	ExitConflict             = 6
	ExitValidation           = 7
	ExitThrottled            = 8
	ExitTransport            = 9
	ExitConfirmationRequired = 10
	ExitAmbiguousCommit      = 11
	// ExitInterrupted follows the shell convention of 128 plus the signal
	// number rather than taking a place in the table above, because an
	// interrupt is the operator's own decision and not a way the command
	// failed.
	ExitInterrupted = 130
)

type contractError struct {
	Code      string
	Message   string
	ExitCode  int
	Retryable bool
	Details   map[string]any
	Cause     error
}

func (e *contractError) Error() string { return e.Message }
func (e *contractError) Unwrap() error { return e.Cause }

func usageError(message string) error {
	return &contractError{Code: "usage", Message: message, ExitCode: ExitUsage}
}

func validationError(code, message string) error {
	return &contractError{Code: code, Message: message, ExitCode: ExitValidation}
}

// argumentUsage names what the command expected. A count on its own says
// nothing about what to pass, which sends a person back to help and leaves an
// unattended caller retrying the same call; the Use line already carries the
// argument names, so the error carries it too.
func argumentUsage(cmd *cobra.Command) string {
	return "; usage: " + cmd.UseLine()
}

func exactArgs(expected int) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if len(args) != expected {
			return usageError(fmt.Sprintf("%s accepts %d argument(s), received %d%s", cmd.CommandPath(), expected, len(args), argumentUsage(cmd)))
		}
		return nil
	}
}

func authRequired(message string) error {
	return &contractError{Code: "authentication_required", Message: message, ExitCode: ExitAuth}
}

func confirmationRequired(message string) error {
	return &contractError{Code: "confirmation_required", Message: message, ExitCode: ExitConfirmationRequired}
}

func classifyError(err error) (*contractError, output.ErrorBody) {
	var known *contractError
	if errors.As(err, &known) {
		return known, output.ErrorBody{Code: known.Code, Message: known.Message, Retryable: known.Retryable, Details: known.Details}
	}
	var fieldErr *output.FieldError
	if errors.As(err, &fieldErr) {
		known = &contractError{Code: "schema", Message: fieldErr.Error(), ExitCode: ExitUsage, Cause: err}
		return known, output.ErrorBody{Code: known.Code, Message: known.Message, Retryable: false}
	}
	var apiErr *taiga.Error
	if errors.As(err, &apiErr) {
		exitCode := ExitValidation
		switch apiErr.Kind {
		case taiga.KindAuth:
			exitCode = ExitAuth
		case taiga.KindForbidden:
			exitCode = ExitForbidden
		case taiga.KindNotFound:
			exitCode = ExitNotFound
		case taiga.KindConflict:
			exitCode = ExitConflict
		case taiga.KindThrottled:
			exitCode = ExitThrottled
		case taiga.KindTransport:
			exitCode = ExitTransport
		case taiga.KindAmbiguousCommit:
			exitCode = ExitAmbiguousCommit
		}
		known = &contractError{Code: string(apiErr.Kind), Message: apiErr.Message, ExitCode: exitCode, Retryable: apiErr.Retryable, Details: apiErr.Details, Cause: err}
		return known, output.ErrorBody{Code: string(apiErr.Kind), Message: apiErr.Message, Retryable: apiErr.Retryable, Details: apiErr.Details, UpstreamStatus: apiErr.UpstreamStatus}
	}
	// Deliberately last of the error checks. A write whose outcome is unknown
	// carries the cancellation that caused it, so testing for cancellation any
	// earlier would report an interrupt and drop the warning that something may
	// already have been committed.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code, message := "interrupted", "the command was interrupted before it finished"
		if errors.Is(err, context.DeadlineExceeded) {
			code, message = "timeout", "the command ran out of time before it finished"
		}
		known = &contractError{Code: code, Message: message, ExitCode: ExitInterrupted, Cause: err}
		return known, output.ErrorBody{Code: code, Message: message, Retryable: false}
	}
	known = &contractError{Code: "internal", Message: fmt.Sprintf("unexpected failure: %v", err), ExitCode: ExitGeneric, Cause: err}
	return known, output.ErrorBody{Code: known.Code, Message: known.Message, Retryable: false}
}
