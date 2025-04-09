package response_info

import (
	"fmt"
	"github.com/go-playground/validator/v10"
	"strings"
)

type ResponseInfo struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

const (
	StatusOK    = "OK"
	StatusError = "Error"
)

func OK() ResponseInfo {
	return ResponseInfo{
		Status: StatusOK,
	}
}

func Error(msg string) ResponseInfo {
	return ResponseInfo{
		Status: StatusError,
		Error:  msg,
	}
}

func ValidationError(errs validator.ValidationErrors) ResponseInfo {
	var errMessages []string
	for _, err := range errs {
		switch err.ActualTag() {
		case "required":
			errMessages = append(errMessages, fmt.Sprintf("field %s is a required field", err.Field()))
		case "skipsConstraint":
			errMessages = append(errMessages, fmt.Sprintf("field %s is not a valid skips number (must be a number >= 0)", err.Field()))
		default:
			errMessages = append(errMessages, fmt.Sprintf("field %s is not valid", err.Field()))
		}
	}

	return ResponseInfo{
		Status: StatusError,
		Error:  strings.Join(errMessages, ", "),
	}
}
