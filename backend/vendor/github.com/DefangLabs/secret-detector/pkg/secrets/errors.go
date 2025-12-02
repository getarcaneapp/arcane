package secrets

import "fmt"

type NotTextFileError struct {
	path string
}

func NewNotTextFileError(path string) error {
	return &NotTextFileError{path: path}
}

func (err *NotTextFileError) Error() string {
	return fmt.Sprintf("%s is not a text file", err.path)
}
