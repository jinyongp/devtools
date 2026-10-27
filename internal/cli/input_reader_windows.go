package cli

import "io"

func cancellableInput(reader io.Reader) (io.Reader, func(), error) {
	return reader, func() {}, nil
}
