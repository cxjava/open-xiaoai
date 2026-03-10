package base

import "fmt"

const VERSION = "1.0.0"

type AppError struct {
	Message string
	Err     error
}

func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Err)
	}
	return e.Message
}

func (e *AppError) Unwrap() error {
	return e.Err
}

func NewError(msg string) *AppError {
	return &AppError{Message: msg}
}

func WrapError(msg string, err error) *AppError {
	return &AppError{Message: msg, Err: err}
}
